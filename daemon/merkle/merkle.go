package merkle

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
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
			ignored := slices.Contains(ignores, entry.Name())
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
		maps.Copy(result, flattenTree(child, fullPath))
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

// GetParentPath returns the parent directory path for a given path.
func GetParentPath(path string) string {
	if path == "" || path == "." {
		return ""
	}

	// Find last separator
	lastSlash := -1
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			lastSlash = i
			break
		}
	}

	if lastSlash == -1 {
		return ""
	}

	return path[:lastSlash]
}

// UpdateNode updates a node at the given path and propagates hash changes to root.
// Returns true if update was successful, false if path not found or error occurred.
func (t *Tree) UpdateNode(rootPath, path string) (bool, error) {
	// Handle root case
	if path == "" || path == "." {
		newTree, err := Build(rootPath, []string{".psync"})
		if err != nil {
			return false, err
		}
		t.Root = newTree.Root
		return true, nil
	}

	// Find parent directory to update its children
	parentPath := GetParentPath(path)
	parent := t.GetNode(parentPath)
	parentExists := parent != nil

	if !parentExists || !parent.IsDir {
		// Parent doesn't exist or is not a directory, need full rebuild
		newTree, err := Build(rootPath, []string{".psync"})
		if err != nil {
			return false, err
		}
		t.Root = newTree.Root
		return true, nil
	}

	// Rebuild this node based on current filesystem
	fullPath := filepath.Join(rootPath, path)

	// Check if file/directory exists
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File was deleted, remove it from parent's children
			t.removeChild(parent, path)
			err := t.propagateHash(parent)
			return true, err
		}
		return false, err
	}

	// Create/update node
	if info.IsDir() {
		// Directory: rebuild it recursively
		node, err := buildNode(rootPath, path, []string{".psync"})
		if err != nil {
			return false, err
		}

		// Update or add child in parent
		t.updateChild(parent, node)
		err = t.propagateHash(parent)
		return true, err
	} else {
		// File: just hash content
		hash, err := hashFile(fullPath)
		if err != nil {
			return false, err
		}

		node := &Node{
			Path:  path,
			Hash:  hash,
			IsDir: false,
			Size:  info.Size(),
		}

		// Update or add child in parent
		t.updateChild(parent, node)
		err = t.propagateHash(parent)
		return true, err
	}
}

// removeChild removes a child node by path from parent.
func (t *Tree) removeChild(parent *Node, childPath string) {
	for i, child := range parent.Children {
		if child.Path == childPath {
			parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
			break
		}
	}
}

// updateChild updates or adds a child node in parent.
func (t *Tree) updateChild(parent *Node, newChild *Node) {
	// Look for existing child
	for i, child := range parent.Children {
		if child.Path == newChild.Path {
			parent.Children[i] = newChild
			return
		}
	}
	// Add new child
	parent.Children = append(parent.Children, newChild)
}

// propagateHash updates directory hashes from given node up to root.
func (t *Tree) propagateHash(node *Node) error {
	current := node

	for current != nil {
		if current.IsDir {
			// Recalculate directory hash from children
			hasher := sha256.New()

			// Sort children for deterministic hashing
			sort.Slice(current.Children, func(i, j int) bool {
				return current.Children[i].Path < current.Children[j].Path
			})

			for _, child := range current.Children {
				hasher.Write([]byte(child.Path))
				hasher.Write([]byte(child.Hash))
			}
			current.Hash = hex.EncodeToString(hasher.Sum(nil))
		}

		// Move to parent
		if current.Path == "" || current.Path == "." {
			break // Reached root
		}

		parentPath := GetParentPath(current.Path)
		parent := t.GetNode(parentPath)
		if parent == nil {
			// Should not happen in well-formed tree
			break
		}

		// Update parent's reference to this child
		t.updateChild(parent, current)
		current = parent
	}

	return nil
}
