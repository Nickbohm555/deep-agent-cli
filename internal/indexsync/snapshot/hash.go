package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Nickbohm555/deep-agent-cli/internal/indexsync"
)

type DirectoryChildHash struct {
	Name     string
	NodeType indexsync.NodeType
	NodeHash string
}

func HashFileContent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", path, err)
	}

	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func HashDirectoryNode(relPath string, children []DirectoryChildHash) (string, error) {
	normalizedPath, err := normalizeDirectoryHashPath(relPath)
	if err != nil {
		return "", err
	}

	parts := make([]string, 0, len(children)+2)
	parts = append(parts, "path="+normalizedPath, "type="+string(indexsync.NodeTypeDir))
	for _, child := range children {
		if strings.TrimSpace(child.Name) == "" {
			return "", fmt.Errorf("hash directory node: child name is required")
		}
		if strings.TrimSpace(child.NodeHash) == "" {
			return "", fmt.Errorf("hash directory node: child hash is required for %q", child.Name)
		}
		parts = append(parts, "child="+child.Name+"|"+string(child.NodeType)+"|"+child.NodeHash)
	}

	return hashCanonicalParts(parts...), nil
}

func HashNodeTree(repoRoot string, root *Node) (string, error) {
	if root == nil {
		return "", fmt.Errorf("hash node tree: root is required")
	}

	if root.Entry.NodeType == "" {
		root.Entry.NodeType = indexsync.NodeTypeDir
	}

	return assignHashes(repoRoot, root, "")
}

func assignHashes(repoRoot string, node *Node, parentHash string) (string, error) {
	node.Entry.ParentHash = parentHash

	switch node.Entry.NodeType {
	case indexsync.NodeTypeFile:
		if strings.TrimSpace(node.Entry.Path) == "" {
			return "", fmt.Errorf("hash file node: path is required")
		}
		contentHash, err := HashFileContent(filepath.Join(repoRoot, filepath.FromSlash(node.Entry.Path)))
		if err != nil {
			return "", err
		}
		node.Entry.ContentHash = contentHash
		node.Entry.NodeHash = hashFileNode(node.Entry, contentHash)
		return node.Entry.NodeHash, nil
	case indexsync.NodeTypeDir:
		childHashes := make([]DirectoryChildHash, 0, len(node.Children))
		for _, child := range node.Children {
			childHash, err := assignHashes(repoRoot, child, "")
			if err != nil {
				return "", err
			}
			child.Entry.ParentHash = node.Entry.NodeHash
			childHash = childHash
			childHashes = append(childHashes, DirectoryChildHash{
				Name:     pathBase(child.Entry.Path),
				NodeType: child.Entry.NodeType,
				NodeHash: childHash,
			})
		}

		nodeHash, err := HashDirectoryNode(node.Entry.Path, childHashes)
		if err != nil {
			return "", err
		}
		node.Entry.NodeHash = nodeHash
		for _, child := range node.Children {
			child.Entry.ParentHash = nodeHash
		}
		return nodeHash, nil
	default:
		return "", fmt.Errorf("hash node tree: unsupported node type %q", node.Entry.NodeType)
	}
}

func hashFileNode(entry Entry, contentHash string) string {
	parts := []string{
		"path=" + entry.Path,
		"type=" + string(indexsync.NodeTypeFile),
		"content=" + contentHash,
	}
	if entry.SizeBytes != nil {
		parts = append(parts, "size="+strconv.FormatInt(*entry.SizeBytes, 10))
	} else {
		parts = append(parts, "size=")
	}

	return hashCanonicalParts(parts...)
}

func hashCanonicalParts(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func normalizeDirectoryHashPath(relPath string) (string, error) {
	if strings.TrimSpace(relPath) == "" {
		return "", nil
	}
	return normalizeSnapshotPath(relPath)
}

func pathBase(relPath string) string {
	if relPath == "" {
		return ""
	}
	parts := strings.Split(relPath, "/")
	return parts[len(parts)-1]
}
