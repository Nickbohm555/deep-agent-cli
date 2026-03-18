package diff

import "strings"

func MaterializeChangedFiles(delta SyncDeltaSet) ChangedFiles {
	materialized := ChangedFiles{}
	upsertSeen := make(map[string]struct{}, len(delta.Changes))
	removeSeen := make(map[string]struct{}, len(delta.Changes))

	for _, change := range delta.Changes {
		path := strings.TrimSpace(change.Path)
		if path == "" {
			continue
		}

		switch change.Op {
		case DeltaOpAdd, DeltaOpModify:
			if _, ok := upsertSeen[path]; ok {
				continue
			}
			upsertSeen[path] = struct{}{}
			materialized.FilesToUpsert = append(materialized.FilesToUpsert, path)
		case DeltaOpDelete:
			if _, ok := removeSeen[path]; ok {
				continue
			}
			removeSeen[path] = struct{}{}
			materialized.FilesToRemove = append(materialized.FilesToRemove, path)
		}
	}

	materialized.sort()
	return materialized
}
