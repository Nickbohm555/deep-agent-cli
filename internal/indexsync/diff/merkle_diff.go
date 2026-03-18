package diff

import (
	"fmt"
	"slices"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync/snapshot"
)

func DiffSnapshots(previous, current *snapshot.Snapshot) (SyncDeltaSet, error) {
	delta := SyncDeltaSet{}
	if previous != nil {
		delta.PreviousRootHash = previous.RootHash
	}
	if current != nil {
		delta.CurrentRootHash = current.RootHash
	}

	if previous != nil && current != nil && previous.RootHash == current.RootHash {
		return delta, nil
	}

	var previousRoot *snapshot.Node
	if previous != nil {
		previousRoot = previous.Root
	}

	var currentRoot *snapshot.Node
	if current != nil {
		currentRoot = current.Root
	}

	if err := DiffNodeRecursive(previousRoot, currentRoot, &delta); err != nil {
		return SyncDeltaSet{}, err
	}

	delta.sort()
	return delta, nil
}

func DiffNodeRecursive(previous, current *snapshot.Node, delta *SyncDeltaSet) error {
	if delta == nil {
		return fmt.Errorf("diff node recursive: delta is required")
	}
	if previous == nil && current == nil {
		return nil
	}
	if previous == nil {
		return collectSubtree(current, DeltaOpAdd, delta)
	}
	if current == nil {
		return collectSubtree(previous, DeltaOpDelete, delta)
	}
	if previous.Entry.NodeType != current.Entry.NodeType {
		if err := collectSubtree(previous, DeltaOpDelete, delta); err != nil {
			return err
		}
		return collectSubtree(current, DeltaOpAdd, delta)
	}
	if previous.Entry.NodeHash != "" && previous.Entry.NodeHash == current.Entry.NodeHash {
		return nil
	}

	switch previous.Entry.NodeType {
	case indexsync.NodeTypeFile:
		delta.add(FileDelta{
			Op:                  DeltaOpModify,
			Path:                current.Entry.Path,
			PreviousNodeHash:    previous.Entry.NodeHash,
			CurrentNodeHash:     current.Entry.NodeHash,
			PreviousContentHash: previous.Entry.ContentHash,
			CurrentContentHash:  current.Entry.ContentHash,
		})
		return nil
	case indexsync.NodeTypeDir:
		return diffChildren(previous, current, delta)
	default:
		return fmt.Errorf("diff node recursive: unsupported node type %q", previous.Entry.NodeType)
	}
}

func diffChildren(previous, current *snapshot.Node, delta *SyncDeltaSet) error {
	previousChildren := childrenByPath(previous.Children)
	currentChildren := childrenByPath(current.Children)
	keys := make([]string, 0, len(previousChildren)+len(currentChildren))
	seen := make(map[string]struct{}, len(previousChildren)+len(currentChildren))
	for path := range previousChildren {
		if _, ok := seen[path]; !ok {
			seen[path] = struct{}{}
			keys = append(keys, path)
		}
	}
	for path := range currentChildren {
		if _, ok := seen[path]; !ok {
			seen[path] = struct{}{}
			keys = append(keys, path)
		}
	}
	slices.Sort(keys)

	for _, path := range keys {
		if err := DiffNodeRecursive(previousChildren[path], currentChildren[path], delta); err != nil {
			return err
		}
	}

	return nil
}

func collectSubtree(node *snapshot.Node, op DeltaOp, delta *SyncDeltaSet) error {
	if node == nil {
		return nil
	}

	switch node.Entry.NodeType {
	case indexsync.NodeTypeDir:
		for _, child := range node.Children {
			if err := collectSubtree(child, op, delta); err != nil {
				return err
			}
		}
		return nil
	case indexsync.NodeTypeFile:
		change := FileDelta{
			Op:   op,
			Path: node.Entry.Path,
		}
		switch op {
		case DeltaOpAdd:
			change.CurrentNodeHash = node.Entry.NodeHash
			change.CurrentContentHash = node.Entry.ContentHash
		case DeltaOpDelete:
			change.PreviousNodeHash = node.Entry.NodeHash
			change.PreviousContentHash = node.Entry.ContentHash
		default:
			return fmt.Errorf("collect subtree: unsupported op %q", op)
		}
		delta.add(change)
		return nil
	default:
		return fmt.Errorf("collect subtree: unsupported node type %q", node.Entry.NodeType)
	}
}

func childrenByPath(children []*snapshot.Node) map[string]*snapshot.Node {
	indexed := make(map[string]*snapshot.Node, len(children))
	for _, child := range children {
		if child == nil {
			continue
		}
		indexed[child.Entry.Path] = child
	}
	return indexed
}
