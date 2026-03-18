package status

import "time"

type JobKind string

const (
	JobKindSync  JobKind = "sync"
	JobKindIndex JobKind = "index"
)

type JobState string

const (
	JobStatePending   JobState = "pending"
	JobStateRunning   JobState = "running"
	JobStateSucceeded JobState = "succeeded"
	JobStateFailed    JobState = "failed"
	JobStateRetryable JobState = "retryable"
)

type Status struct {
	SessionID             string        `json:"session_id"`
	RepoRoot              string        `json:"repo_root"`
	LatestSnapshot        SnapshotInfo  `json:"latest_snapshot"`
	LastSuccessfulSyncAt  *time.Time    `json:"last_successful_sync_at,omitempty"`
	LastSuccessfulIndexAt *time.Time    `json:"last_successful_index_at,omitempty"`
	LastDeltaSize         int           `json:"last_delta_size"`
	Queue                 QueueCounts   `json:"queue"`
	Running               RunningJobs   `json:"running"`
	LatestError           *ErrorSummary `json:"latest_error,omitempty"`
}

type SnapshotInfo struct {
	ID          int64      `json:"id,omitempty"`
	RootHash    string     `json:"root_hash,omitempty"`
	Status      string     `json:"status,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type QueueCounts struct {
	PendingSyncJobs  int `json:"pending_sync_jobs"`
	PendingIndexJobs int `json:"pending_index_jobs"`
	RunningSyncJobs  int `json:"running_sync_jobs"`
	RunningIndexJobs int `json:"running_index_jobs"`
}

type RunningJobs struct {
	Sync  *JobSummary `json:"sync,omitempty"`
	Index *JobSummary `json:"index,omitempty"`
}

type JobSummary struct {
	JobID      int64      `json:"job_id"`
	Kind       JobKind    `json:"kind"`
	State      JobState   `json:"state"`
	Attempt    int        `json:"attempt"`
	EnqueuedAt *time.Time `json:"enqueued_at,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	SnapshotID *int64     `json:"snapshot_id,omitempty"`
	RootHash   string     `json:"root_hash,omitempty"`
	DeltaSize  int        `json:"delta_size,omitempty"`
}

type ErrorSummary struct {
	JobID      int64      `json:"job_id"`
	Kind       JobKind    `json:"kind"`
	State      JobState   `json:"state"`
	Message    string     `json:"message"`
	OccurredAt *time.Time `json:"occurred_at,omitempty"`
	Attempt    int        `json:"attempt"`
	SnapshotID *int64     `json:"snapshot_id,omitempty"`
	RootHash   string     `json:"root_hash,omitempty"`
}

type JobRecord struct {
	JobID      int64
	Kind       JobKind
	State      JobState
	Attempt    int
	EnqueuedAt *time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
	SnapshotID *int64
	RootHash   string
	DeltaSize  int
	Error      string
}
