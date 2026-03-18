package river

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

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

	return &Client{river: riverClient}, nil
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
