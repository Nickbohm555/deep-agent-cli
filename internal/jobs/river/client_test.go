package river

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
	indexstatus "github.com/Nickbohm555/deep-agent-cli/internal/indexsync/status"
	"github.com/Nickbohm555/deep-agent-cli/internal/jobs/contracts"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/rivertype"
)

func TestEnqueueSyncJobDeduplicatesBySessionAndRepo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	harness := newRiverHarness(t)
	defer harness.Close()

	client, err := NewClient(harness.NewPool(t), Config{Schema: harness.schemaName})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	repoRoot := filepath.Join(harness.repoRoot, "sync")
	first, err := client.EnqueueSyncJob(ctx, contracts.SyncJobPayload{
		SessionID:   "session-1",
		RepoRoot:    repoRoot,
		RequestedAt: time.Unix(1700000000, 0).UTC(),
		Trigger:     "manual",
	})
	if err != nil {
		t.Fatalf("first EnqueueSyncJob returned error: %v", err)
	}

	second, err := client.EnqueueSyncJob(ctx, contracts.SyncJobPayload{
		SessionID:   "session-1",
		RepoRoot:    repoRoot,
		RequestedAt: time.Unix(1700003600, 0).UTC(),
		Trigger:     "watcher",
	})
	if err != nil {
		t.Fatalf("second EnqueueSyncJob returned error: %v", err)
	}

	if first.Duplicate {
		t.Fatal("first sync enqueue marked as duplicate")
	}
	if !second.Duplicate {
		t.Fatal("second sync enqueue was not marked as duplicate")
	}
	if got := harness.CountJobs(t, kindSyncJob); got != 1 {
		t.Fatalf("sync job row count = %d, want 1", got)
	}
	if got := harness.JobQueue(t, kindSyncJob); got != QueueSyncJobs {
		t.Fatalf("sync queue = %q, want %q", got, QueueSyncJobs)
	}
}

func TestEnqueueIndexJobDeduplicatesBySessionAndRepo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	harness := newRiverHarness(t)
	defer harness.Close()

	client, err := NewClient(harness.NewPool(t), Config{Schema: harness.schemaName})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	repoRoot := filepath.Join(harness.repoRoot, "index")
	first, err := client.EnqueueIndexJob(ctx, contracts.IndexJobPayload{
		SessionID:   "session-2",
		RepoRoot:    repoRoot,
		SnapshotID:  41,
		RootHash:    "root-a",
		RequestedAt: time.Unix(1700000000, 0).UTC(),
		Delta: indexsync.SyncDelta{
			Changes: []indexsync.DeltaRecord{{Path: "docs/a.md", Action: indexsync.DeltaActionAdd}},
		},
	})
	if err != nil {
		t.Fatalf("first EnqueueIndexJob returned error: %v", err)
	}

	second, err := client.EnqueueIndexJob(ctx, contracts.IndexJobPayload{
		SessionID:   "session-2",
		RepoRoot:    repoRoot,
		SnapshotID:  42,
		RootHash:    "root-b",
		RequestedAt: time.Unix(1700007200, 0).UTC(),
		Delta: indexsync.SyncDelta{
			Changes: []indexsync.DeltaRecord{{Path: "docs/b.md", Action: indexsync.DeltaActionModify}},
		},
	})
	if err != nil {
		t.Fatalf("second EnqueueIndexJob returned error: %v", err)
	}

	if first.Duplicate {
		t.Fatal("first index enqueue marked as duplicate")
	}
	if !second.Duplicate {
		t.Fatal("second index enqueue was not marked as duplicate")
	}
	if got := harness.CountJobs(t, kindIndexJob); got != 1 {
		t.Fatalf("index job row count = %d, want 1", got)
	}
	if got := harness.JobQueue(t, kindIndexJob); got != QueueIndexJobs {
		t.Fatalf("index queue = %q, want %q", got, QueueIndexJobs)
	}
}

func TestListJobsReturnsScopedStatusRecords(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	harness := newRiverHarness(t)
	defer harness.Close()

	client, err := NewClient(harness.NewPool(t), Config{Schema: harness.schemaName})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	repoRoot := filepath.Join(harness.repoRoot, "status")
	if _, err := client.EnqueueSyncJob(ctx, contracts.SyncJobPayload{
		SessionID: "session-1",
		RepoRoot:  repoRoot,
		Trigger:   "manual",
	}); err != nil {
		t.Fatalf("EnqueueSyncJob returned error: %v", err)
	}
	if _, err := client.EnqueueIndexJob(ctx, contracts.IndexJobPayload{
		SessionID:  "session-1",
		RepoRoot:   repoRoot,
		SnapshotID: 17,
		RootHash:   "root-17",
		Delta: indexsync.SyncDelta{
			Changes: []indexsync.DeltaRecord{
				{Path: "README.md", Action: indexsync.DeltaActionModify},
				{Path: "docs/guide.md", Action: indexsync.DeltaActionAdd},
			},
		},
	}); err != nil {
		t.Fatalf("EnqueueIndexJob returned error: %v", err)
	}
	if _, err := client.EnqueueSyncJob(ctx, contracts.SyncJobPayload{
		SessionID: "session-2",
		RepoRoot:  filepath.Join(harness.repoRoot, "other"),
		Trigger:   "watcher",
	}); err != nil {
		t.Fatalf("EnqueueSyncJob(other scope) returned error: %v", err)
	}

	startedAt := time.Unix(1700000100, 0).UTC()
	if _, err := harness.adminPool.Exec(
		ctx,
		fmt.Sprintf("UPDATE %s.river_job SET state = $2, attempt = $3, attempted_at = $4 WHERE kind = $1", harness.schemaName),
		kindIndexJob,
		rivertype.JobStateRunning,
		1,
		startedAt,
	); err != nil {
		t.Fatalf("UPDATE running river_job returned error: %v", err)
	}

	jobs, err := client.ListJobs(ctx, "session-1", repoRoot)
	if err != nil {
		t.Fatalf("ListJobs returned error: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("ListJobs count = %d, want 2", len(jobs))
	}

	var sawSync bool
	var sawIndex bool
	for _, job := range jobs {
		switch job.Kind {
		case indexstatus.JobKindSync:
			sawSync = true
			if job.State != indexstatus.JobStatePending {
				t.Fatalf("sync job state = %q, want pending", job.State)
			}
		case indexstatus.JobKindIndex:
			sawIndex = true
			if job.State != indexstatus.JobStateRunning {
				t.Fatalf("index job state = %q, want running", job.State)
			}
			if job.SnapshotID == nil || *job.SnapshotID != 17 {
				t.Fatalf("index snapshot_id = %v, want 17", job.SnapshotID)
			}
			if job.RootHash != "root-17" {
				t.Fatalf("index root_hash = %q, want root-17", job.RootHash)
			}
			if job.DeltaSize != 2 {
				t.Fatalf("index delta_size = %d, want 2", job.DeltaSize)
			}
		}
	}
	if !sawSync || !sawIndex {
		t.Fatalf("jobs = %+v, want sync and index records", jobs)
	}
}

func TestListJobsReturnsLatestAttemptError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	harness := newRiverHarness(t)
	defer harness.Close()

	client, err := NewClient(harness.NewPool(t), Config{Schema: harness.schemaName})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	repoRoot := filepath.Join(harness.repoRoot, "failure")
	if _, err := client.EnqueueIndexJob(ctx, contracts.IndexJobPayload{
		SessionID:  "session-9",
		RepoRoot:   repoRoot,
		SnapshotID: 42,
		RootHash:   "root-42",
		Delta: indexsync.SyncDelta{
			Changes: []indexsync.DeltaRecord{{Path: "broken.go", Action: indexsync.DeltaActionModify}},
		},
	}); err != nil {
		t.Fatalf("EnqueueIndexJob returned error: %v", err)
	}

	errorsJSON, err := json.Marshal([]rivertype.AttemptError{
		{Attempt: 1, Error: "first failure", At: time.Unix(1700000200, 0).UTC()},
		{Attempt: 2, Error: "latest failure", At: time.Unix(1700000300, 0).UTC()},
	})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	finishedAt := time.Unix(1700000300, 0).UTC()
	if _, err := harness.adminPool.Exec(
		ctx,
		fmt.Sprintf("UPDATE %s.river_job SET state = $2, attempt = $3, finalized_at = $4, errors = $5::jsonb WHERE kind = $1", harness.schemaName),
		kindIndexJob,
		rivertype.JobStateRetryable,
		2,
		finishedAt,
		string(errorsJSON),
	); err != nil {
		t.Fatalf("UPDATE retryable river_job returned error: %v", err)
	}

	jobs, err := client.ListJobs(ctx, "session-9", repoRoot)
	if err != nil {
		t.Fatalf("ListJobs returned error: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("ListJobs count = %d, want 1", len(jobs))
	}
	if jobs[0].State != indexstatus.JobStateRetryable {
		t.Fatalf("job state = %q, want retryable", jobs[0].State)
	}
	if jobs[0].Error != "latest failure" {
		t.Fatalf("job error = %q, want latest failure", jobs[0].Error)
	}
}

type riverHarness struct {
	adminPool   *pgxpool.Pool
	databaseURL string
	schemaName  string
	repoRoot    string
}

func newRiverHarness(t *testing.T) *riverHarness {
	t.Helper()

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for river integration tests")
	}

	schemaName := fmt.Sprintf("test_river_%d", time.Now().UnixNano())
	adminPool := newRiverTestPool(t, databaseURL, "")
	if _, err := adminPool.Exec(context.Background(), "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("CREATE SCHEMA returned error: %v", err)
	}

	harness := &riverHarness{
		adminPool:   adminPool,
		databaseURL: databaseURL,
		schemaName:  schemaName,
		repoRoot:    filepath.Join("/tmp", schemaName),
	}

	if err := MigrateSchema(context.Background(), harness.NewPool(t), schemaName); err != nil {
		t.Fatalf("MigrateSchema returned error: %v", err)
	}

	return harness
}

func (h *riverHarness) Close() {
	if h.adminPool == nil {
		return
	}

	_, _ = h.adminPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+h.schemaName+" CASCADE")
	h.adminPool.Close()
}

func (h *riverHarness) NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := newRiverTestPool(t, h.databaseURL, h.schemaName)
	t.Cleanup(pool.Close)
	return pool
}

func (h *riverHarness) CountJobs(t *testing.T, kind string) int {
	t.Helper()

	var count int
	err := h.adminPool.QueryRow(
		context.Background(),
		fmt.Sprintf("SELECT COUNT(*) FROM %s.river_job WHERE kind = $1", h.schemaName),
		kind,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count river_job returned error: %v", err)
	}

	return count
}

func (h *riverHarness) JobQueue(t *testing.T, kind string) string {
	t.Helper()

	var queue string
	err := h.adminPool.QueryRow(
		context.Background(),
		fmt.Sprintf("SELECT queue FROM %s.river_job WHERE kind = $1 ORDER BY id ASC LIMIT 1", h.schemaName),
		kind,
	).Scan(&queue)
	if err != nil {
		t.Fatalf("query river_job queue returned error: %v", err)
	}

	return queue
}

func newRiverTestPool(t *testing.T, databaseURL, schemaName string) *pgxpool.Pool {
	t.Helper()

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	if schemaName != "" {
		config.ConnConfig.RuntimeParams["search_path"] = fmt.Sprintf("%s,public", schemaName)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("Ping returned error: %v", err)
	}

	return pool
}
