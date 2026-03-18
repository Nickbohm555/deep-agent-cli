package indexsync

import (
	"strings"
	"time"
)

type SnapshotStatus string

const (
	SnapshotStatusPending    SnapshotStatus = "pending"
	SnapshotStatusActive     SnapshotStatus = "active"
	SnapshotStatusSuperseded SnapshotStatus = "superseded"
	SnapshotStatusFailed     SnapshotStatus = "failed"
)

type NodeType string

const (
	NodeTypeFile NodeType = "file"
	NodeTypeDir  NodeType = "dir"
)

type FileStatus string

const (
	FileStatusActive  FileStatus = "active"
	FileStatusDeleted FileStatus = "deleted"
)

type DeltaAction string

const (
	DeltaActionAdd    DeltaAction = "add"
	DeltaActionModify DeltaAction = "modify"
	DeltaActionDelete DeltaAction = "delete"
)

type SnapshotRoot struct {
	ID               int64
	SessionID        string
	RepoRoot         string
	RootHash         string
	ParentSnapshotID *int64
	Status           SnapshotStatus
	IsActive         bool
	CreatedAt        time.Time
	CompletedAt      *time.Time
}

type MerkleNode struct {
	ID          int64
	SnapshotID  int64
	Path        string
	ParentPath  string
	NodeType    NodeType
	NodeHash    string
	ParentHash  string
	ContentHash string
	SizeBytes   *int64
	MTimeNS     *int64
	Status      FileStatus
	CreatedAt   time.Time
}

type FileState struct {
	ID             int64
	SessionID      string
	RepoRoot       string
	RelPath        string
	LastSnapshotID *int64
	ContentHash    string
	NodeHash       string
	ParentHash     string
	ChunkSetHash   string
	SizeBytes      *int64
	MTimeNS        *int64
	Status         FileStatus
	DeletedAt      *time.Time
	UpdatedAt      time.Time
}

type DeltaRecord struct {
	Path                string
	Action              DeltaAction
	NodeType            NodeType
	PreviousNodeHash    string
	CurrentNodeHash     string
	PreviousContentHash string
	CurrentContentHash  string
}

type SyncDelta struct {
	SessionID          string
	RepoRoot           string
	PreviousSnapshotID *int64
	CurrentSnapshotID  *int64
	PreviousRootHash   string
	CurrentRootHash    string
	Changes            []DeltaRecord
}

func (d SyncDelta) ChangedPaths() []string {
	if len(d.Changes) == 0 {
		return nil
	}

	paths := make([]string, 0, len(d.Changes))
	for _, change := range d.Changes {
		if strings.TrimSpace(change.Path) == "" {
			continue
		}
		paths = append(paths, change.Path)
	}

	return paths
}
