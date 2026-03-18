package runtime

import (
	"fmt"
	"strings"
	"time"

	indexstatus "github.com/Nickbohm555/deep-agent-cli/internal/indexsync/status"
)

func RenderIndexStatusSummary(status indexstatus.Status) string {
	parts := []string{renderBackgroundWork(status)}

	if snapshot := renderLatestSnapshot(status); snapshot != "" {
		parts = append(parts, snapshot)
	}
	if syncLine := renderLastSync(status.LastSuccessfulSyncAt); syncLine != "" {
		parts = append(parts, syncLine)
	}
	if indexLine := renderLastIndex(status.LastSuccessfulIndexAt, status.LastDeltaSize); indexLine != "" {
		parts = append(parts, indexLine)
	}
	if status.LatestError != nil && strings.TrimSpace(status.LatestError.Message) != "" {
		parts = append(parts, fmt.Sprintf("Latest error: %s", strings.TrimSpace(status.LatestError.Message)))
	}

	return strings.Join(parts, " ")
}

func renderBackgroundWork(status indexstatus.Status) string {
	queue := status.Queue
	parts := make([]string, 0, 4)
	if queue.RunningSyncJobs > 0 {
		parts = append(parts, fmt.Sprintf("%d sync running", queue.RunningSyncJobs))
	}
	if queue.PendingSyncJobs > 0 {
		parts = append(parts, fmt.Sprintf("%d sync pending", queue.PendingSyncJobs))
	}
	if queue.RunningIndexJobs > 0 {
		parts = append(parts, fmt.Sprintf("%d index running", queue.RunningIndexJobs))
	}
	if queue.PendingIndexJobs > 0 {
		parts = append(parts, fmt.Sprintf("%d index pending", queue.PendingIndexJobs))
	}
	if len(parts) == 0 {
		return "Background work is idle."
	}

	return "Background work: " + strings.Join(parts, ",") + "."
}

func renderLatestSnapshot(status indexstatus.Status) string {
	if status.LatestSnapshot.ID == 0 && strings.TrimSpace(status.LatestSnapshot.RootHash) == "" && strings.TrimSpace(status.LatestSnapshot.Status) == "" {
		return ""
	}

	parts := []string{"Latest snapshot:"}
	if status.LatestSnapshot.ID != 0 {
		parts = append(parts, fmt.Sprintf("#%d", status.LatestSnapshot.ID))
	}
	if strings.TrimSpace(status.LatestSnapshot.Status) != "" {
		parts = append(parts, strings.TrimSpace(status.LatestSnapshot.Status))
	}
	if strings.TrimSpace(status.LatestSnapshot.RootHash) != "" {
		parts = append(parts, strings.TrimSpace(status.LatestSnapshot.RootHash))
	}

	return strings.Join(parts, " ") + "."
}

func renderLastSync(at *time.Time) string {
	if at == nil {
		return ""
	}
	return "Last sync: " + at.UTC().Format(time.RFC3339) + "."
}

func renderLastIndex(at *time.Time, deltaSize int) string {
	if at == nil {
		return ""
	}
	if deltaSize > 0 {
		return fmt.Sprintf("Last index: %s (%d changed files).", at.UTC().Format(time.RFC3339), deltaSize)
	}
	return "Last index: " + at.UTC().Format(time.RFC3339) + "."
}
