package snapshot

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
	"github.com/Nickbohm555/deep-agent-cli/internal/session"
)

type Entry struct {
	Path        string
	ParentPath  string
	NodeType    indexsync.NodeType
	NodeHash    string
	ParentHash  string
	ContentHash string
	SizeBytes   *int64
	MTimeNS     *int64
}

type Node struct {
	Entry    Entry
	Children []*Node
}

type Snapshot struct {
	RepoRoot string
	Entries  []Entry
	Root     *Node
	RootHash string
}

func BuildSnapshot(repoRoot string) (*Snapshot, error) {
	canonicalRoot, err := session.CanonicalizeRepoRoot(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("build snapshot: %w", err)
	}

	entries := make([]Entry, 0)
	err = filepath.WalkDir(canonicalRoot, func(path string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == canonicalRoot {
			return nil
		}

		relPath, err := filepath.Rel(canonicalRoot, path)
		if err != nil {
			return fmt.Errorf("relative path for %q: %w", path, err)
		}

		normalizedPath, err := normalizeSnapshotPath(relPath)
		if err != nil {
			return fmt.Errorf("normalize snapshot path %q: %w", relPath, err)
		}

		if dirEntry.IsDir() {
			if shouldSkipDir(dirEntry.Name()) {
				return filepath.SkipDir
			}
			entries = append(entries, Entry{
				Path:       normalizedPath,
				ParentPath: parentPath(normalizedPath),
				NodeType:   indexsync.NodeTypeDir,
			})
			return nil
		}

		if !ShouldIndexPath(normalizedPath) || !isPathWithinRoot(canonicalRoot, path) {
			return nil
		}

		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat %q: %w", normalizedPath, err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		size := info.Size()
		mtime := info.ModTime().UTC().UnixNano()
		entries = append(entries, Entry{
			Path:       normalizedPath,
			ParentPath: parentPath(normalizedPath),
			NodeType:   indexsync.NodeTypeFile,
			SizeBytes:  &size,
			MTimeNS:    &mtime,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("build snapshot under %q: %w", canonicalRoot, err)
	}

	sortEntries(entries)
	root, err := BuildNodeTree(entries)
	if err != nil {
		return nil, err
	}

	rootHash, err := HashNodeTree(canonicalRoot, root)
	if err != nil {
		return nil, fmt.Errorf("build snapshot hashes: %w", err)
	}

	hashedEntries := make(map[string]Entry, len(entries))
	var collect func(node *Node)
	collect = func(node *Node) {
		for _, child := range node.Children {
			hashedEntries[child.Entry.Path] = child.Entry
			collect(child)
		}
	}
	collect(root)
	for i, entry := range entries {
		if hashed, ok := hashedEntries[entry.Path]; ok {
			entries[i] = hashed
		}
	}

	return &Snapshot{
		RepoRoot: canonicalRoot,
		Entries:  entries,
		Root:     root,
		RootHash: rootHash,
	}, nil
}

func BuildNodeTree(entries []Entry) (*Node, error) {
	root := &Node{
		Entry: Entry{
			NodeType: indexsync.NodeTypeDir,
		},
	}

	if len(entries) == 0 {
		return root, nil
	}

	sorted := append([]Entry(nil), entries...)
	sortEntries(sorted)

	nodesByPath := map[string]*Node{
		"": root,
	}

	for _, entry := range sorted {
		normalizedPath, err := normalizeSnapshotPath(entry.Path)
		if err != nil {
			return nil, fmt.Errorf("build node tree: %w", err)
		}

		entry.Path = normalizedPath
		entry.ParentPath = parentPath(normalizedPath)

		parentNode, ok := nodesByPath[entry.ParentPath]
		if !ok {
			return nil, fmt.Errorf("build node tree: missing parent for %q", entry.Path)
		}

		node := &Node{Entry: entry}
		parentNode.Children = append(parentNode.Children, node)
		nodesByPath[entry.Path] = node
	}

	sortTree(root)
	return root, nil
}

func (s *Snapshot) OrderedPaths() []string {
	if s == nil || s.Root == nil {
		return nil
	}

	paths := make([]string, 0, len(s.Entries))
	var walk func(node *Node)
	walk = func(node *Node) {
		for _, child := range node.Children {
			paths = append(paths, child.Entry.Path)
			walk(child)
		}
	}
	walk(s.Root)
	return paths
}

func sortEntries(entries []Entry) {
	slices.SortFunc(entries, func(a, b Entry) int {
		aDepth := pathDepth(a.Path)
		bDepth := pathDepth(b.Path)
		if aDepth != bDepth {
			return aDepth - bDepth
		}
		if cmp := strings.Compare(a.Path, b.Path); cmp != 0 {
			return cmp
		}
		if a.NodeType == b.NodeType {
			return 0
		}
		if a.NodeType == indexsync.NodeTypeDir {
			return -1
		}
		return 1
	})
}

func sortTree(node *Node) {
	if node == nil {
		return
	}

	slices.SortFunc(node.Children, func(a, b *Node) int {
		if cmp := strings.Compare(a.Entry.Path, b.Entry.Path); cmp != 0 {
			return cmp
		}
		if a.Entry.NodeType == b.Entry.NodeType {
			return 0
		}
		if a.Entry.NodeType == indexsync.NodeTypeDir {
			return -1
		}
		return 1
	})

	for _, child := range node.Children {
		sortTree(child)
	}
}

func parentPath(relPath string) string {
	dir := filepath.Dir(relPath)
	if dir == "." {
		return ""
	}
	return filepath.ToSlash(dir)
}

func pathDepth(relPath string) int {
	if relPath == "" {
		return 0
	}
	return strings.Count(relPath, "/") + 1
}

func isPathWithinRoot(root, candidate string) bool {
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return false
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
