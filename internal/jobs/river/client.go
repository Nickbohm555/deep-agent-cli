package river

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	indexstatus "github.com/Nickbohm555/deep-agent-cli/internal/indexsync/status"
	"github.com/Nickbohm555/deep-agent-cli/internal/jobs/contracts"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	riverqueue "github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
)

const (
	kindSyncJob  = "index_sync"
	kindIndexJob = "index_apply_delta"
)

type Config struct {
	Schema   string
	ClientID string
	Logger   *slog.Logger
}

type Client struct {
	river *riverqueue.Client[pgx.Tx]
	query func(context.Context, string, ...any) (pgx.Rows, error)
}

type EnqueueResult struct {
	JobID       int64
	Duplicate   bool
	Kind        string
	Queue       string
	ScheduledAt time.Time
}

func MigrateSchema(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	if pool == nil {
		return fmt.Errorf("river migrations require a database pool")
	}

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{
		Schema: strings.TrimSpace(schema),
	})
	if err != nil {
		return fmt.Errorf("create river migrator: %w", err)
	}

	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("migrate river schema: %w", err)
	}

	return nil
}

func NewClient(pool *pgxpool.Pool, cfg Config) (*Client, error) {
	if pool == nil {
		return nil, fmt.Errorf("river client requires a database pool")
	}

	riverClient, err := riverqueue.NewClient(riverpgxv5.New(pool), &riverqueue.Config{
		ID:     strings.TrimSpace(cfg.ClientID),
		Logger: cfg.Logger,
		Schema: strings.TrimSpace(cfg.Schema),
	})
	if err != nil {
		return nil, fmt.Errorf("create river client: %w", err)
	}

	return &Client{
		river: riverClient,
		query: pool.Query,
	}, nil
}

func (c *Client) EnqueueSyncJob(ctx context.Context, payload contracts.SyncJobPayload) (EnqueueResult, error) {
	args := newSyncJobArgs(payload)
	result, err := c.river.Insert(ctx, args, nil)
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("enqueue sync job: %w", err)
	}

	return enqueueResultFromInsert(result), nil
}

func (c *Client) EnqueueIndexJob(ctx context.Context, payload contracts.IndexJobPayload) (EnqueueResult, error) {
	args := newIndexJobArgs(payload)
	result, err := c.river.Insert(ctx, args, nil)
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("enqueue index job: %w", err)
	}

	return enqueueResultFromInsert(result), nil
}

const listScopedJobsSQL = `
	SELECT
		id,
		kind,
		state,
		attempt,
		created_at,
		attempted_at,
		finalized_at,
		encoded_args,
		errors::jsonb
	FROM river_job
	WHERE
		kind = ANY($1) AND
		encoded_args->'payload'->>'session_id' = $2 AND
		encoded_args->'payload'->>'repo_root' = $3
	ORDER BY created_at DESC, id DESC
`

func (c *Client) ListJobs(ctx context.Context, sessionID, repoRoot string) ([]indexstatus.JobRecord, error) {
	scopeSessionID := strings.TrimSpace(sessionID)
	scopeRepoRoot := normalizeRepoRoot(repoRoot)
	if scopeSessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if scopeRepoRoot == "" {
		return nil, fmt.Errorf("repo_root is required")
	}
	if c == nil || c.query == nil {
		return nil, fmt.Errorf("river client query dependencies are not configured")
	}

	rows, err := c.query(ctx, listScopedJobsSQL, []string{kindSyncJob, kindIndexJob}, scopeSessionID, scopeRepoRoot)
	if err != nil {
		return nil, fmt.Errorf("query scoped river jobs: %w", err)
	}
	defer rows.Close()

	var records []indexstatus.JobRecord
	for rows.Next() {
		var (
			record      indexstatus.JobRecord
			kind        string
			state       rivertype.JobState
			encodedArgs []byte
			errorsJSON  []byte
		)
		if err := rows.Scan(
			&record.JobID,
			&kind,
			&state,
			&record.Attempt,
			&record.EnqueuedAt,
			&record.StartedAt,
			&record.FinishedAt,
			&encodedArgs,
			&errorsJSON,
		); err != nil {
			return nil, fmt.Errorf("scan scoped river job: %w", err)
		}

		record.Kind = mapStatusKind(kind)
		record.State = mapStatusState(state)
		record.Error = latestAttemptError(errorsJSON)

		if err := decodeJobPayload(kind, encodedArgs, &record); err != nil {
			return nil, fmt.Errorf("decode scoped river job %d payload: %w", record.JobID, err)
		}

		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scoped river jobs: %w", err)
	}

	return records, nil
}

type syncJobArgs struct {
	IdentityKey string                   `json:"identity_key" river:"unique"`
	Payload     contracts.SyncJobPayload `json:"payload"`
}

func newSyncJobArgs(payload contracts.SyncJobPayload) syncJobArgs {
	payload.Purpose = firstNonEmpty(payload.Purpose, contracts.JobPurposeSyncScan)
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	payload.RepoRoot = normalizeRepoRoot(payload.RepoRoot)
	payload.Trigger = strings.TrimSpace(payload.Trigger)
	if payload.RequestedAt.IsZero() {
		payload.RequestedAt = time.Now().UTC()
	}

	return syncJobArgs{
		IdentityKey: payload.IdentityKey(),
		Payload:     payload,
	}
}

func (syncJobArgs) Kind() string {
	return kindSyncJob
}

func (syncJobArgs) InsertOpts() riverqueue.InsertOpts {
	return riverqueue.InsertOpts{
		Priority: prioritySyncJob,
		Queue:    QueueSyncJobs,
		UniqueOpts: riverqueue.UniqueOpts{
			ByArgs: true,
		},
	}
}

type indexJobArgs struct {
	IdentityKey string                    `json:"identity_key" river:"unique"`
	Payload     contracts.IndexJobPayload `json:"payload"`
}

func newIndexJobArgs(payload contracts.IndexJobPayload) indexJobArgs {
	payload.Purpose = firstNonEmpty(payload.Purpose, contracts.JobPurposeIndexApplyDelta)
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	payload.RepoRoot = normalizeRepoRoot(payload.RepoRoot)
	if payload.RequestedAt.IsZero() {
		payload.RequestedAt = time.Now().UTC()
	}

	return indexJobArgs{
		IdentityKey: payload.IdentityKey(),
		Payload:     payload,
	}
}

func (indexJobArgs) Kind() string {
	return kindIndexJob
}

func (indexJobArgs) InsertOpts() riverqueue.InsertOpts {
	return riverqueue.InsertOpts{
		Priority: priorityIndexJob,
		Queue:    QueueIndexJobs,
		UniqueOpts: riverqueue.UniqueOpts{
			ByArgs: true,
		},
	}
}

func enqueueResultFromInsert(result *rivertype.JobInsertResult) EnqueueResult {
	if result == nil || result.Job == nil {
		return EnqueueResult{}
	}

	return EnqueueResult{
		JobID:       result.Job.ID,
		Duplicate:   result.UniqueSkippedAsDuplicate,
		Kind:        result.Job.Kind,
		Queue:       result.Job.Queue,
		ScheduledAt: result.Job.ScheduledAt,
	}
}

func mapStatusKind(kind string) indexstatus.JobKind {
	switch kind {
	case kindIndexJob:
		return indexstatus.JobKindIndex
	default:
		return indexstatus.JobKindSync
	}
}

func mapStatusState(state rivertype.JobState) indexstatus.JobState {
	switch state {
	case rivertype.JobStateRunning:
		return indexstatus.JobStateRunning
	case rivertype.JobStateCompleted:
		return indexstatus.JobStateSucceeded
	case rivertype.JobStateRetryable:
		return indexstatus.JobStateRetryable
	case rivertype.JobStateCancelled, rivertype.JobStateDiscarded:
		return indexstatus.JobStateFailed
	default:
		return indexstatus.JobStatePending
	}
}

func decodeJobPayload(kind string, encodedArgs []byte, record *indexstatus.JobRecord) error {
	switch kind {
	case kindSyncJob:
		var args syncJobArgs
		if err := json.Unmarshal(encodedArgs, &args); err != nil {
			return err
		}
		return nil
	case kindIndexJob:
		var args indexJobArgs
		if err := json.Unmarshal(encodedArgs, &args); err != nil {
			return err
		}
		if args.Payload.SnapshotID != 0 {
			record.SnapshotID = &args.Payload.SnapshotID
		}
		record.RootHash = strings.TrimSpace(args.Payload.RootHash)
		record.DeltaSize = len(args.Payload.Delta.Changes)
		return nil
	default:
		return fmt.Errorf("unsupported river job kind %q", kind)
	}
}

func latestAttemptError(errorsJSON []byte) string {
	if len(errorsJSON) == 0 {
		return ""
	}

	var attempts []rivertype.AttemptError
	if err := json.Unmarshal(errorsJSON, &attempts); err != nil || len(attempts) == 0 {
		return ""
	}

	return strings.TrimSpace(attempts[len(attempts)-1].Error)
}

func normalizeRepoRoot(repoRoot string) string {
	trimmed := strings.TrimSpace(repoRoot)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}
