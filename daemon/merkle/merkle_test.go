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

	tree, err := Build(dir, nil)
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

	tree1, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("First Build failed: %v", err)
	}

	tree2, err := Build(dir, nil)
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

	tree1, err := Build(dir, nil)
	if err != nil {
		t.Fatalf("First Build failed: %v", err)
	}
	originalHash := tree1.Root.Hash

	// Modify a file
	if err := os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("modified content"), 0644); err != nil {
		t.Fatal(err)
	}

	tree2, err := Build(dir, nil)
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

	local, _ := Build(dir1, nil)
	remote, _ := Build(dir2, nil)

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

	local, _ := Build(dir1, nil)
	remote, _ := Build(dir2, nil)

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

	local, _ := Build(dir1, nil)
	remote, _ := Build(dir2, nil)

	_, _, deleted := Diff(local.Root, remote.Root)

	if len(deleted) != 1 {
		t.Errorf("Expected 1 deleted file (file3.txt), got %d: %v", len(deleted), deleted)
	}
}

func TestGetNode(t *testing.T) {
	dir := setupTestDir(t)

	tree, err := Build(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Find file1.txt
	node := tree.GetNode("file1.txt")
	if node == nil {
		t.Error("file1.txt not found")
	} else if node.IsDir {
		t.Error("file1.txt should not be a directory")
	}

	// Find subdir
	node = tree.GetNode("subdir")
	if node == nil {
		t.Error("subdir not found")
	} else if !node.IsDir {
		t.Error("subdir should be a directory")
	}

	// Find nested file
	node = tree.GetNode(filepath.Join("subdir", "file3.txt"))
	if node == nil {
		t.Error("subdir/file3.txt not found")
	}
}

func TestEmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	tree, err := Build(dir, []string{".psync"})
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

func TestUpdateNodeFileCreation(t *testing.T) {
	dir := t.TempDir()

	// Build initial empty tree
	tree, err := Build(dir, []string{".psync"})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	originalHash := tree.Root.Hash

	// Create a file
	filePath := filepath.Join(dir, "newfile.txt")
	err = os.WriteFile(filePath, []byte("hello world"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Update incrementally
	updated, err := tree.UpdateNode(dir, "newfile.txt")
	if err != nil {
		t.Fatalf("UpdateNode failed: %v", err)
	}
	if !updated {
		t.Error("UpdateNode should return true for new file")
	}

	// Hash should change
	if tree.Root.Hash == originalHash {
		t.Error("Root hash should change after adding file")
	}

	// Verify with full rebuild
	fullRebuild, err := Build(dir, []string{".psync"})
	if err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}
	if tree.Root.Hash != fullRebuild.Root.Hash {
		t.Errorf("Incremental hash mismatch. Got %s, want %s", tree.Root.Hash, fullRebuild.Root.Hash)
	}

	// Verify node exists
	node := tree.GetNode("newfile.txt")
	if node == nil {
		t.Error("newfile.txt should exist in tree")
	}
	if node.IsDir {
		t.Error("newfile.txt should be a file, not directory")
	}
}

func TestUpdateNodeFileModification(t *testing.T) {
	dir := setupTestDir(t)

	// Build initial tree
	tree, err := Build(dir, []string{".psync"})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	originalHash := tree.Root.Hash

	// Modify existing file
	filePath := filepath.Join(dir, "file1.txt")
	err = os.WriteFile(filePath, []byte("modified content"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Update incrementally
	updated, err := tree.UpdateNode(dir, "file1.txt")
	if err != nil {
		t.Fatalf("UpdateNode failed: %v", err)
	}
	if !updated {
		t.Error("UpdateNode should return true for modified file")
	}

	// Hash should change
	if tree.Root.Hash == originalHash {
		t.Error("Root hash should change after file modification")
	}

	// Verify with full rebuild
	fullRebuild, err := Build(dir, []string{".psync"})
	if err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}
	if tree.Root.Hash != fullRebuild.Root.Hash {
		t.Errorf("Incremental hash mismatch after modification. Got %s, want %s", tree.Root.Hash, fullRebuild.Root.Hash)
	}

	// Verify node updated
	node := tree.GetNode("file1.txt")
	if node == nil {
		t.Error("file1.txt should exist in tree")
	}
	expectedHash, _ := hashFile(filePath)
	if node.Hash != expectedHash {
		t.Errorf("File hash incorrect. Got %s, want %s", node.Hash, expectedHash)
	}
}

func TestUpdateNodeFileDeletion(t *testing.T) {
	dir := setupTestDir(t)

	// Build initial tree
	tree, err := Build(dir, []string{".psync"})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	originalHash := tree.Root.Hash

	// Delete a file
	filePath := filepath.Join(dir, "file1.txt")
	err = os.Remove(filePath)
	if err != nil {
		t.Fatal(err)
	}

	// Update incrementally
	updated, err := tree.UpdateNode(dir, "file1.txt")
	if err != nil {
		t.Fatalf("UpdateNode failed: %v", err)
	}
	if !updated {
		t.Error("UpdateNode should return true for deleted file")
	}

	// Hash should change
	if tree.Root.Hash == originalHash {
		t.Error("Root hash should change after file deletion")
	}

	// Verify with full rebuild
	fullRebuild, err := Build(dir, []string{".psync"})
	if err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}
	if tree.Root.Hash != fullRebuild.Root.Hash {
		t.Errorf("Incremental hash mismatch after deletion. Got %s, want %s", tree.Root.Hash, fullRebuild.Root.Hash)
	}

	// Verify node removed
	node := tree.GetNode("file1.txt")
	if node != nil {
		t.Error("file1.txt should not exist in tree after deletion")
	}

	// Other files should still exist
	if tree.GetNode("file2.txt") == nil {
		t.Error("file2.txt should still exist")
	}
}

func TestUpdateNodeDirectoryCreation(t *testing.T) {
	dir := t.TempDir()

	// Build initial empty tree
	tree, err := Build(dir, []string{".psync"})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	originalHash := tree.Root.Hash

	// Create a directory
	subDirPath := filepath.Join(dir, "newdir")
	err = os.Mkdir(subDirPath, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Update incrementally
	updated, err := tree.UpdateNode(dir, "newdir")
	if err != nil {
		t.Fatalf("UpdateNode failed: %v", err)
	}
	if !updated {
		t.Error("UpdateNode should return true for new directory")
	}

	// Hash should change (empty directories have specific hash)
	if tree.Root.Hash == originalHash {
		t.Error("Root hash should change after adding directory")
	}

	// Verify with full rebuild
	fullRebuild, err := Build(dir, []string{".psync"})
	if err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}
	if tree.Root.Hash != fullRebuild.Root.Hash {
		t.Errorf("Incremental hash mismatch after directory creation. Got %s, want %s", tree.Root.Hash, fullRebuild.Root.Hash)
	}

	// Verify node exists
	node := tree.GetNode("newdir")
	if node == nil {
		t.Error("newdir should exist in tree")
	}
	if !node.IsDir {
		t.Error("newdir should be a directory")
	}
}

func TestUpdateNodeNonExistentFile(t *testing.T) {
	dir := t.TempDir()

	tree, err := Build(dir, []string{".psync"})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Try to update non-existent file
	_, err = tree.UpdateNode(dir, "nonexistent.txt")
	if err != nil {
		t.Fatalf("UpdateNode should handle non-existent files gracefully: %v", err)
	}

	// Verify with full rebuild
	fullRebuild, err := Build(dir, []string{".psync"})
	if err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}
	if tree.Root.Hash != fullRebuild.Root.Hash {
		t.Errorf("Hash mismatch after updating non-existent file. Got %s, want %s", tree.Root.Hash, fullRebuild.Root.Hash)
	}
}

func TestUpdateNodeWithIgnorePatterns(t *testing.T) {
	dir := t.TempDir()

	// Build initial tree
	tree, err := Build(dir, []string{".psync"})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Create a file that should be ignored
	ignoredPath := filepath.Join(dir, ".psync", "internal.txt")
	err = os.MkdirAll(filepath.Dir(ignoredPath), 0755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(ignoredPath, []byte("should be ignored"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Update should handle ignored files correctly
	_, err = tree.UpdateNode(dir, filepath.Join(".psync", "internal.txt"))
	if err != nil {
		t.Fatalf("UpdateNode should handle ignored files: %v", err)
	}

	// Verify with full rebuild (should not include ignored file)
	fullRebuild, err := Build(dir, []string{".psync"})
	if err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}
	if tree.Root.Hash != fullRebuild.Root.Hash {
		t.Errorf("Hash mismatch with ignored files. Got %s, want %s", tree.Root.Hash, fullRebuild.Root.Hash)
	}
}

func TestMultipleUpdates(t *testing.T) {
	dir := t.TempDir()

	// Build initial tree
	tree, err := Build(dir, []string{".psync"})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Create multiple files
	files := []string{"file1.txt", "file2.txt", "file3.txt"}
	for _, filename := range files {
		filePath := filepath.Join(dir, filename)
		err := os.WriteFile(filePath, []byte("content "+filename), 0644)
		if err != nil {
			t.Fatal(err)
		}

		// Update incrementally
		_, err = tree.UpdateNode(dir, filename)
		if err != nil {
			t.Fatalf("UpdateNode failed for %s: %v", filename, err)
		}
	}

	// Verify with full rebuild
	fullRebuild, err := Build(dir, []string{".psync"})
	if err != nil {
		t.Fatalf("Rebuild failed: %v", err)
	}
	if tree.Root.Hash != fullRebuild.Root.Hash {
		t.Errorf("Hash mismatch after multiple updates. Got %s, want %s", tree.Root.Hash, fullRebuild.Root.Hash)
	}

	// Verify all files exist
	for _, filename := range files {
		node := tree.GetNode(filename)
		if node == nil {
			t.Errorf("File %s should exist in tree", filename)
		}
		if node.IsDir {
			t.Errorf("File %s should be a file, not directory", filename)
		}
	}
}
