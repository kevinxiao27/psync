package merkle

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Node represents a node in the Merkle tree.
type Node struct {
	Path     string  `json:"path"`               // Relative path from root
	Hash     string  `json:"hash"`               // SHA-256 hash
	IsDir    bool    `json:"is_dir"`             // True for directories
	Size     int64   `json:"size"`               // File size (0 for dirs)
	Children []*Node `json:"children,omitempty"` // Child nodes (nil for files)
}

// Tree represents a Merkle tree for a directory structure.
type Tree struct {
	Root *Node
}

// Build constructs a Merkle tree from the given root path.
func Build(rootPath string, ignores []string) (*Tree, error) {
	root, err := buildNode(rootPath, "", ignores)
	if err != nil {
		return nil, err
	}
	return &Tree{Root: root}, nil
}

// buildNode recursively builds a node and its children.
func buildNode(basePath, relativePath string, ignores []string) (*Node, error) {
	fullPath := filepath.Join(basePath, relativePath)
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}

	node := &Node{
		Path:  relativePath,
		IsDir: info.IsDir(),
		Size:  info.Size(),
	}

	if info.IsDir() {
		entries, err := os.ReadDir(fullPath)
		if err != nil {
			return nil, err
		}

		// Sort for deterministic hashing
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})

		hasher := sha256.New()
		for _, entry := range entries {
			// Check ignores
			ignored := false
			for _, ignore := range ignores {
				if entry.Name() == ignore {
					ignored = true
					break
				}
			}
			// Also ignore temporary files
			if strings.HasSuffix(entry.Name(), ".tmp") {
				ignored = true
			}
			if ignored {
				continue
			}

			childPath := filepath.Join(relativePath, entry.Name())
			child, err := buildNode(basePath, childPath, ignores)
			if err != nil {
				return nil, err
			}
			node.Children = append(node.Children, child)
			// Include child hash in directory hash
			hasher.Write([]byte(child.Path))
			hasher.Write([]byte(child.Hash))
		}
		node.Hash = hex.EncodeToString(hasher.Sum(nil))
	} else {
		// File: hash content
		hash, err := hashFile(fullPath)
		if err != nil {
			return nil, err
		}
		node.Hash = hash
	}

	return node, nil
}

// hashFile computes SHA-256 of a file's content.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// Diff compares two trees and returns paths that differ.
// Returns slices of: added, modified, deleted paths.
func Diff(local, remote *Node) (added, modified, deleted []string) {
	localMap := flattenTree(local, "")
	remoteMap := flattenTree(remote, "")

	// Find added and modified
	for path, localNode := range localMap {
		remoteNode, exists := remoteMap[path]
		if !exists {
			deleted = append(deleted, path) // In local but not in remote = deleted remotely
		} else if localNode.Hash != remoteNode.Hash {
			modified = append(modified, path)
		}
	}

	// Find added (in remote but not local)
	for path := range remoteMap {
		if _, exists := localMap[path]; !exists {
			added = append(added, path)
		}
	}

	return added, modified, deleted
}

// flattenTree creates a map of path -> node for all file nodes.
func flattenTree(node *Node, basePath string) map[string]*Node {
	result := make(map[string]*Node)
	if node == nil {
		return result
	}

	fullPath := filepath.Join(basePath, node.Path)
	if fullPath == "" {
		fullPath = "."
	}

	if !node.IsDir {
		result[fullPath] = node
	}

	for _, child := range node.Children {
		for k, v := range flattenTree(child, fullPath) {
			result[k] = v
		}
	}

	return result
}

// GetNode finds a node by relative path.
func (t *Tree) GetNode(path string) *Node {
	node, _ := findNode(t.Root, path)
	return node
}

func findNode(node *Node, targetPath string) (*Node, bool) {
	if node == nil {
		return nil, false
	}
	if node.Path == targetPath {
		return node, true
	}
	for _, child := range node.Children {
		if found, ok := findNode(child, targetPath); ok {
			return found, true
		}
	}
	return nil, false
}
