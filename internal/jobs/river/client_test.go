package river

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
	"github.com/Nickbohm555/deep-agent-cli/internal/jobs/contracts"
	"github.com/jackc/pgx/v5/pgxpool"
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
