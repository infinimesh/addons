package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type fakeWriter struct {
	bytes.Buffer
	closeErr error
	closed   bool
}

func (w *fakeWriter) Close() error { w.closed = true; return w.closeErr }

func TestUploadMapping(t *testing.T) {
	p := filepath.Join(t.TempDir(), "1.csv")
	_ = os.WriteFile(p, []byte("d1,pump\n"), 0o644)
	for _, closeErr := range []error{nil, errors.New("upload failed")} {
		f, _ := os.Open(p)
		var name string
		w := &fakeWriter{closeErr: closeErr}
		err := uploader(func(n string) io.WriteCloser { name = n; return w })(f, "d1/1.csv")
		f.Close()
		if !errors.Is(err, closeErr) {
			t.Errorf("err = %v, want %v", err, closeErr)
		}
		if name != "d1/1.csv" || w.String() != "d1,pump\n" || !w.closed {
			t.Errorf("object %q body %q closed %v", name, w.String(), w.closed)
		}
	}
}
