package runtime

import (
	"strings"
	"testing"
	"time"

	indexstatus "github.com/Nickbohm555/deep-agent-cli/internal/indexsync/status"
)

func TestRenderIndexStatusSummaryIncludesProgressAndHistory(t *testing.T) {
	t.Parallel()

	syncAt := time.Unix(1700000000, 0).UTC()
	indexAt := syncAt.Add(time.Minute)

	summary := RenderIndexStatusSummary(indexstatus.Status{
		LatestSnapshot: indexstatus.SnapshotInfo{
			ID:       17,
			RootHash: "root-17",
			Status:   "active",
		},
		LastSuccessfulSyncAt:  &syncAt,
		LastSuccessfulIndexAt: &indexAt,
		LastDeltaSize:         4,
		Queue: indexstatus.QueueCounts{
			RunningSyncJobs:  1,
			PendingIndexJobs: 2,
		},
	})

	for _, want := range []string{
		"Background work: 1 sync running,2 index pending.",
		"Latest snapshot: #17 active root-17.",
		"Last sync: 2023-11-14T22:13:20Z.",
		"Last index: 2023-11-14T22:14:20Z (4 changed files).",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary = %q, want substring %q", summary, want)
		}
	}
}

func TestRenderIndexStatusSummaryIncludesIdleAndLatestError(t *testing.T) {
	t.Parallel()

	summary := RenderIndexStatusSummary(indexstatus.Status{
		LatestError: &indexstatus.ErrorSummary{
			Message: "embedding refresh timeout",
		},
	})

	if !strings.Contains(summary, "Background work is idle.") {
		t.Fatalf("summary = %q, want idle state", summary)
	}
	if !strings.Contains(summary, "Latest error: embedding refresh timeout") {
		t.Fatalf("summary = %q, want latest error", summary)
	}
}
