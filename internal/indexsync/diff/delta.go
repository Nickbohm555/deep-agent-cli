package diff

import "slices"

type DeltaOp string

const (
	DeltaOpAdd    DeltaOp = "add"
	DeltaOpModify DeltaOp = "modify"
	DeltaOpDelete DeltaOp = "delete"
)

type FileDelta struct {
	Op                  DeltaOp
	Path                string
	PreviousNodeHash    string
	CurrentNodeHash     string
	PreviousContentHash string
	CurrentContentHash  string
}

type SyncDeltaSet struct {
	PreviousRootHash string
	CurrentRootHash  string
	Changes          []FileDelta
}

func (d *SyncDeltaSet) add(change FileDelta) {
	d.Changes = append(d.Changes, change)
}

func (d *SyncDeltaSet) sort() {
	slices.SortFunc(d.Changes, func(a, b FileDelta) int {
		if a.Path != b.Path {
			if a.Path < b.Path {
				return -1
			}
			return 1
		}
		return compareDeltaOp(a.Op, b.Op)
	})
}

func compareDeltaOp(a, b DeltaOp) int {
	order := func(op DeltaOp) int {
		switch op {
		case DeltaOpAdd:
			return 0
		case DeltaOpModify:
			return 1
		case DeltaOpDelete:
			return 2
		default:
			return 3
		}
	}

	return order(a) - order(b)
}
