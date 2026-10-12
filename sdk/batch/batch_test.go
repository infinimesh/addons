package batch

import (
	"errors"
	"testing"
)

func TestFlushKeepsFailedItems(t *testing.T) {
	var got [][]int
	fail := true
	b := &Batcher[int]{Size: 2, MaxBuffer: 10, Flush: func(v []int) error {
		if fail {
			return errors.New("down")
		}
		got = append(got, v)
		return nil
	}}
	for i := 0; i < 3; i++ {
		b.Add(i)
	}
	if b.FlushOnce() || b.Len() != 3 {
		t.Fatalf("failed flush should keep items, len %d", b.Len())
	}
	fail = false
	for b.FlushOnce() {
	}
	if len(got) != 2 || len(got[0]) != 2 || got[1][0] != 2 || b.Len() != 0 {
		t.Fatalf("got %v, len %d", got, b.Len())
	}
}

func TestMaxBufferDropsOldest(t *testing.T) {
	b := &Batcher[int]{Size: 100, MaxBuffer: 3, Flush: func([]int) error { return nil }}
	for i := 0; i < 5; i++ {
		b.Add(i)
	}
	if b.Len() != 3 || b.buf[0] != 2 {
		t.Fatalf("buf = %v", b.buf)
	}
}
