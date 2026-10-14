package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/runner"
	"github.com/infinimesh/addons/sdk/runner/runnertest"
	shadowpb "github.com/infinimesh/proto/shadow"
)

func readAll(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	files, _ := filepath.Glob(filepath.Join(dir, "*.csv"))
	for _, f := range files {
		c, _ := os.ReadFile(f)
		b.Write(c)
	}
	return b.String()
}

func TestSinkOnNamespaceStream(t *testing.T) {
	dir := t.TempDir()
	runnertest.Scenario(t, func(ctx context.Context, a api.Handler) {
		runner.Serve(ctx, a, newSink(dir).handle, time.Hour, shadowpb.StateKey_REPORTED)
	}, []shadowpb.StateKey{shadowpb.StateKey_REPORTED})
	d1, d2 := readAll(t, filepath.Join(dir, "d1")), readAll(t, filepath.Join(dir, "d2"))
	if strings.Count(d1, "\n") != 1 || !strings.Contains(d1, `""temp"":1}`) {
		t.Errorf("d1 csv = %q, want one row with temp 1", d1)
	}
	if strings.Count(d2, "\n") != 1 || !strings.Contains(d2, "d2,pump,device") || !strings.Contains(d2, `""temp"":2}`) {
		t.Errorf("d2 csv = %q, want one row with temp 2", d2)
	}
}
