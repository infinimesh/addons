package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type poster struct {
	secret     string
	client     *http.Client
	maxRetries int
	backoff    time.Duration
	maxBackoff time.Duration
	sleep      func(context.Context, time.Duration) error
}

func newPoster(secret string) *poster {
	return &poster{
		secret:     secret,
		client:     &http.Client{Timeout: 10 * time.Second},
		maxRetries: 5,
		backoff:    200 * time.Millisecond,
		maxBackoff: 30 * time.Second,
		sleep:      sleepCtx,
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *poster) send(ctx context.Context, url string, body []byte) error {
	wait := p.backoff
	var last error
	tries := p.maxRetries
	if tries < 0 {
		tries = 0
	}
	for attempt := 0; attempt <= tries; attempt++ {
		if attempt > 0 {
			if err := p.sleep(ctx, wait); err != nil {
				return err
			}
			if wait *= 2; wait > p.maxBackoff {
				wait = p.maxBackoff
			}
		}
		status, err := p.post(ctx, url, body)
		switch {
		case err != nil:
			last = err
		case status >= 200 && status < 300:
			return nil
		case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500:
			last = fmt.Errorf("status %d", status)
		default:
			return fmt.Errorf("status %d", status)
		}
	}
	return fmt.Errorf("gave up after %d tries: %w", tries+1, last)
}

func (p *poster) post(ctx context.Context, url string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "infinimesh-ota-webhook")
	req.Header.Set(signatureHeader, sign(p.secret, body))
	res, err := p.client.Do(req)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	res.Body.Close()
	return res.StatusCode, nil
}
