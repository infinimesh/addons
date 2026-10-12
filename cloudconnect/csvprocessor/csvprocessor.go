package csvprocessor

import (
	"log"
	"os"
	"path/filepath"
	"time"
)

const envReadDir = "READ_DIR"

// WalkLoop walks READ_DIR every 5 seconds.
func WalkLoop(handleFile func(f *os.File, name string) error) {
	readDir := os.Getenv(envReadDir)
	for range time.Tick(5 * time.Second) {
		log.Printf("walking %s for new files...", readDir)
		err := filepath.Walk(readDir, WalkFunc(readDir, handleFile))
		if err != nil {
			log.Printf("failed to walk directory: %v\n", err)
		}
	}
}

// ObjectName is the upload name of a spool file: its path below the spool
// folder with forward slashes, for example "<device uid>/1791792480.csv".
func ObjectName(readDir, path string) string {
	rel, err := filepath.Rel(readDir, path)
	if err != nil {
		rel = filepath.Base(path)
	}
	return filepath.ToSlash(rel)
}

// WalkFunc hands each file that has not changed for a minute to handleFile,
// and deletes it when handleFile returns nil.
func WalkFunc(readDir string, handleFile func(f *os.File, name string) error) filepath.WalkFunc {
	return func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("failed to read %s: %v\n", path, err)
			return nil
		}
		if info.IsDir() {
			return nil
		}
		// only process files that have not been modified recently
		if !time.Now().Add(-1 * time.Minute).After(info.ModTime()) {
			return nil
		}
		log.Println("processing file:", path)
		f, err := os.Open(path)
		if err != nil {
			log.Printf("failed to open %s: %v\n", path, err)
			return nil
		}
		err = handleFile(f, ObjectName(readDir, path))
		f.Close()
		if err != nil {
			log.Printf("failed to handle file %s, keeping it: %v\n", path, err)
			return nil
		}
		log.Printf("successfully handled file: %s\n", path)
		if err := os.Remove(path); err != nil {
			log.Printf("failed to remove %s: %v\n", path, err)
		} else {
			log.Printf("successfully deleted %s\n", path)
		}
		return nil
	}
}
