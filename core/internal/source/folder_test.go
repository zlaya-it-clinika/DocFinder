package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRead_FolderKeepsSupportedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("пункт 1.1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sheet.xlsx"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	listing, err := Read_Folder(dir)
	if err != nil || len(listing.Files) != 1 || listing.Files[0].Name != "note.txt" || string(listing.Files[0].Body) != "пункт 1.1" {
		t.Fatalf("listing %+v err %v", listing, err)
	}
	if len(listing.Skipped) != 2 {
		t.Fatalf("skipped %+v", listing.Skipped)
	}
	if _, err := Read_Folder(filepath.Join(dir, "..", "secret")); err != ErrUnsafePath {
		t.Fatalf("parent path %v", err)
	}
}
