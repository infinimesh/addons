package csvprocessor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestObjectName(t *testing.T) {
	for _, dir := range []string{"/data", "/data/"} {
		if got := ObjectName(dir, "/data/d1/1791792480.csv"); got != "d1/1791792480.csv" {
			t.Errorf("ObjectName(%q) = %q", dir, got)
		}
	}
}

func TestWalkKeepsFileOnError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d1", "1.csv")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte("x"), 0o644)
	old := time.Now().Add(-2 * time.Minute)
	_ = os.Chtimes(p, old, old)

	var names []string
	fail := func(f *os.File, name string) error { names = append(names, name); return errors.New("upload failed") }
	_ = filepath.Walk(dir, WalkFunc(dir, fail))
	if _, err := os.Stat(p); err != nil {
		t.Fatal("file should stay after a failed upload")
	}
	ok := func(f *os.File, name string) error { return nil }
	_ = filepath.Walk(dir, WalkFunc(dir, ok))
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file should be deleted after a good upload")
	}
	if len(names) != 1 || names[0] != "d1/1.csv" {
		t.Errorf("names = %v", names)
	}
}
