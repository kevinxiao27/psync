package merkle

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTestDir creates a temporary directory structure for testing.
func setupTestDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	// Create structure:
	// dir/
	//   file1.txt
	//   file2.txt
	//   subdir/
	//     file3.txt

	if err := os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("content1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file2.txt"), []byte("content2"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subdir", "file3.txt"), []byte("content3"), 0644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestBuildTree(t *testing.T) {
	dir := setupTestDir(t)

	tree, err := Build(dir)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if tree.Root == nil {
		t.Fatal("Root is nil")
	}

	if !tree.Root.IsDir {
		t.Error("Root should be a directory")
	}

	if tree.Root.Hash == "" {
		t.Error("Root hash should not be empty")
	}

	// Should have 3 children at root level: file1.txt, file2.txt, subdir
	if len(tree.Root.Children) != 3 {
		t.Errorf("Expected 3 children, got %d", len(tree.Root.Children))
	}
}

func TestHashConsistency(t *testing.T) {
	dir := setupTestDir(t)

	tree1, err := Build(dir)
	if err != nil {
		t.Fatalf("First Build failed: %v", err)
	}

	tree2, err := Build(dir)
	if err != nil {
		t.Fatalf("Second Build failed: %v", err)
	}

	// Same directory should produce same hash
	if tree1.Root.Hash != tree2.Root.Hash {
		t.Errorf("Hash inconsistency: %s != %s", tree1.Root.Hash, tree2.Root.Hash)
	}
}

func TestHashChangesOnModification(t *testing.T) {
	dir := setupTestDir(t)

	tree1, err := Build(dir)
	if err != nil {
		t.Fatalf("First Build failed: %v", err)
	}
	originalHash := tree1.Root.Hash

	// Modify a file
	if err := os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("modified content"), 0644); err != nil {
		t.Fatal(err)
	}

	tree2, err := Build(dir)
	if err != nil {
		t.Fatalf("Second Build failed: %v", err)
	}

	if tree2.Root.Hash == originalHash {
		t.Error("Hash should change after file modification")
	}
}

func TestDiffDetectsAddedFiles(t *testing.T) {
	dir1 := setupTestDir(t)
	dir2 := t.TempDir()

	// Copy structure but add a new file
	os.WriteFile(filepath.Join(dir2, "file1.txt"), []byte("content1"), 0644)
	os.WriteFile(filepath.Join(dir2, "file2.txt"), []byte("content2"), 0644)
	os.Mkdir(filepath.Join(dir2, "subdir"), 0755)
	os.WriteFile(filepath.Join(dir2, "subdir", "file3.txt"), []byte("content3"), 0644)
	os.WriteFile(filepath.Join(dir2, "newfile.txt"), []byte("new content"), 0644) // New file

	local, _ := Build(dir1)
	remote, _ := Build(dir2)

	added, _, _ := Diff(local.Root, remote.Root)

	if len(added) != 1 {
		t.Errorf("Expected 1 added file, got %d: %v", len(added), added)
	}
}

func TestDiffDetectsModifiedFiles(t *testing.T) {
	dir1 := setupTestDir(t)
	dir2 := t.TempDir()

	// Same structure but modified content
	os.WriteFile(filepath.Join(dir2, "file1.txt"), []byte("MODIFIED"), 0644) // Modified
	os.WriteFile(filepath.Join(dir2, "file2.txt"), []byte("content2"), 0644)
	os.Mkdir(filepath.Join(dir2, "subdir"), 0755)
	os.WriteFile(filepath.Join(dir2, "subdir", "file3.txt"), []byte("content3"), 0644)

	local, _ := Build(dir1)
	remote, _ := Build(dir2)

	_, modified, _ := Diff(local.Root, remote.Root)

	foundModified := false
	for _, path := range modified {
		if filepath.Base(path) == "file1.txt" {
			foundModified = true
			break
		}
	}
	if !foundModified {
		t.Errorf("Expected file1.txt to be modified, got: %v", modified)
	}
}

func TestDiffDetectsDeletedFiles(t *testing.T) {
	dir1 := setupTestDir(t)
	dir2 := t.TempDir()

	// Only file1.txt and file2.txt, no subdir
	os.WriteFile(filepath.Join(dir2, "file1.txt"), []byte("content1"), 0644)
	os.WriteFile(filepath.Join(dir2, "file2.txt"), []byte("content2"), 0644)

	local, _ := Build(dir1)
	remote, _ := Build(dir2)

	_, _, deleted := Diff(local.Root, remote.Root)

	if len(deleted) != 1 {
		t.Errorf("Expected 1 deleted file (file3.txt), got %d: %v", len(deleted), deleted)
	}
}

func TestGetNode(t *testing.T) {
	dir := setupTestDir(t)

	tree, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Find file1.txt
	node, ok := tree.GetNode("file1.txt")
	if !ok {
		t.Error("file1.txt not found")
	}
	if node.IsDir {
		t.Error("file1.txt should not be a directory")
	}

	// Find subdir
	node, ok = tree.GetNode("subdir")
	if !ok {
		t.Error("subdir not found")
	}
	if !node.IsDir {
		t.Error("subdir should be a directory")
	}

	// Find nested file
	node, ok = tree.GetNode(filepath.Join("subdir", "file3.txt"))
	if !ok {
		t.Error("subdir/file3.txt not found")
	}
}

func TestEmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	tree, err := Build(dir)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if tree.Root == nil {
		t.Fatal("Root should not be nil for empty directory")
	}

	if len(tree.Root.Children) != 0 {
		t.Errorf("Expected 0 children for empty dir, got %d", len(tree.Root.Children))
	}
}
