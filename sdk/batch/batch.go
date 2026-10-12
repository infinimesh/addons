// Package batch groups items and writes them in one call, either when the
// batch is full or when the flush interval passes.
package batch

import (
	"log"
	"sync"
	"time"
)

// Batcher collects items from many goroutines and hands them to Flush.
// When Flush fails the items stay in the buffer and go out with the next
// flush. When the buffer holds more than MaxBuffer items the oldest are
// dropped.
type Batcher[T any] struct {
	Flush     func([]T) error
	Size      int
	Interval  time.Duration
	MaxBuffer int

	mu    sync.Mutex
	buf   []T
	drops int
	kick  chan struct{}
	flush sync.Mutex
}

// Add queues one item. It never blocks on the database.
func (b *Batcher[T]) Add(v T) {
	b.mu.Lock()
	b.buf = append(b.buf, v)
	if over := len(b.buf) - b.MaxBuffer; b.MaxBuffer > 0 && over > 0 {
		b.buf = b.buf[over:]
		b.drops += over
		log.Printf("batch buffer full, dropped %d oldest rows\n", over)
	}
	full := len(b.buf) >= b.Size
	b.mu.Unlock()
	if full && b.kick != nil {
		select {
		case b.kick <- struct{}{}:
		default:
		}
	}
}

// Run flushes on every interval and whenever a batch is full. It does not
// return.
func (b *Batcher[T]) Run() {
	b.mu.Lock()
	b.kick = make(chan struct{}, 1)
	b.mu.Unlock()
	t := time.NewTicker(b.Interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		case <-b.kick:
		}
		for b.FlushOnce() {
		}
	}
}

// FlushOnce writes up to Size items. It returns true if a full batch went
// out and more items are waiting.
func (b *Batcher[T]) FlushOnce() bool {
	b.flush.Lock()
	defer b.flush.Unlock()
	b.mu.Lock()
	n := min(len(b.buf), b.Size)
	items := append([]T(nil), b.buf[:n]...)
	drops := b.drops
	b.mu.Unlock()
	if n == 0 {
		return false
	}
	if err := b.Flush(items); err != nil {
		log.Printf("batch write of %d rows failed, will retry: %s\n", n, err)
		return false
	}
	b.mu.Lock()
	// Add may have dropped some of the items we sent while we were writing.
	sent := max(n-(b.drops-drops), 0)
	b.buf = b.buf[sent:]
	more := len(b.buf) > 0 && n == b.Size
	b.mu.Unlock()
	return more
}

// Len is the number of queued items.
func (b *Batcher[T]) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buf)
}
