package main

import (
	"testing"
	"time"
)

func FuzzWriteUntil(f *testing.F) {
	f.Add(int64(0))
	f.Fuzz(func(t *testing.T, sec int64) {
		if sec < 0 {
			sec = -sec
		}
		sec = sec % (1 << 32)
		in := time.Unix(sec, 0).UTC()
		got := writeUntil(in)
		if got.Before(in) {
			t.Fatalf("writeUntil(%v)=%v", in, got)
		}
	})
}
