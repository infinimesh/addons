package otacore

import (
	"context"
	"math/rand/v2"
	"time"
)

const retryBase = 200 * time.Millisecond

// RetryWait is the delay before the next attempt. attempt 0 waits 200ms,
// then 400ms, then 800ms, plus up to a quarter of that as jitter.
// retryAfter replaces the delay when it is longer, as with a 429 Retry-After.
func RetryWait(attempt int, retryAfter time.Duration) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	d := retryBase << attempt
	if retryAfter > d {
		d = retryAfter
	}
	if extra := int64(d / 4); extra > 0 {
		d += time.Duration(rand.Int64N(extra))
	}
	return d
}

func (r Runner) sleep(ctx context.Context, d time.Duration) error {
	if r.Sleep != nil {
		return r.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
