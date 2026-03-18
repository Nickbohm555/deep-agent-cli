package contracts

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
)

const (
	JobPurposeSyncScan        = "sync.scan"
	JobPurposeIndexApplyDelta = "index.apply_delta"
)

type SyncJobPayload struct {
	SessionID   string    `json:"session_id"`
	RepoRoot    string    `json:"repo_root"`
	Purpose     string    `json:"purpose"`
	Trigger     string    `json:"trigger,omitempty"`
	RequestedAt time.Time `json:"requested_at"`
}

func (p SyncJobPayload) IdentityKey() string {
	return JobIdentityKey(p.Purpose, p.SessionID, p.RepoRoot)
}

type IndexJobPayload struct {
	SessionID   string              `json:"session_id"`
	RepoRoot    string              `json:"repo_root"`
	Purpose     string              `json:"purpose"`
	SnapshotID  int64               `json:"snapshot_id"`
	RootHash    string              `json:"root_hash"`
	Delta       indexsync.SyncDelta `json:"delta"`
	RequestedAt time.Time           `json:"requested_at"`
}

func (p IndexJobPayload) IdentityKey() string {
	return JobIdentityKey(p.Purpose, p.SessionID, p.RepoRoot)
}

func JobIdentityKey(purpose, sessionID, repoRoot string) string {
	normalizedPurpose := normalizePart(purpose)
	normalizedSession := normalizePart(sessionID)
	normalizedRepo := normalizeRepoRoot(repoRoot)
	return strings.Join([]string{normalizedPurpose, normalizedSession, normalizedRepo}, "::")
}

func normalizeRepoRoot(repoRoot string) string {
	trimmed := strings.TrimSpace(repoRoot)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}

func normalizePart(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
