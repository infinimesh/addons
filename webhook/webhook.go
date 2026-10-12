package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/infinimesh/addons/sdk/config"
)

const (
	signatureHeader = "X-Infinimesh-Signature"
	deviceHeader    = "X-Infinimesh-Device"
)

type settings struct {
	URL          string
	Secret       string
	Timeout      time.Duration
	MaxRetries   int
	Backoff      time.Duration
	MaxBackoff   time.Duration
	OnlyReported bool
}

func loadSettings() (*settings, error) {
	s := &settings{URL: config.Env("WEBHOOK_URL"), Secret: config.Env("WEBHOOK_SECRET")}
	u, err := url.Parse(s.URL)
	if s.URL == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("WEBHOOK_URL must be an http or https URL")
	}
	if s.Timeout, err = config.Seconds(10*time.Second, "WEBHOOK_TIMEOUT_SECS"); err != nil {
		return nil, err
	}
	if s.MaxRetries, err = config.Int("WEBHOOK_MAX_RETRIES", 5); err != nil {
		return nil, err
	}
	ms, err := config.Int("WEBHOOK_BACKOFF_MS", 500)
	if err != nil {
		return nil, err
	}
	s.Backoff = time.Duration(ms) * time.Millisecond
	if s.MaxBackoff, err = config.Seconds(30*time.Second, "WEBHOOK_MAX_BACKOFF_SECS"); err != nil {
		return nil, err
	}
	if s.OnlyReported, err = config.Bool("WEBHOOK_ONLY_REPORTED", false); err != nil {
		return nil, err
	}
	return s, nil
}

// sign returns the signature header value: "sha256=" and the hex HMAC-SHA256
// of the body with the secret.
func sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// retryable tells if a response status is worth another try.
func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500
}

type sender struct {
	s      *settings
	client *http.Client
	sleep  func(time.Duration)
}

func newSender(s *settings) *sender {
	return &sender{s: s, client: &http.Client{Timeout: s.Timeout}, sleep: time.Sleep}
}

// send posts one body. It retries network errors, 408, 429 and 5xx with
// doubling backoff, up to MaxRetries extra tries. Other statuses are final.
func (x *sender) send(device string, body []byte) error {
	wait := x.s.Backoff
	var last error
	for attempt := 0; attempt <= x.s.MaxRetries; attempt++ {
		if attempt > 0 {
			x.sleep(wait)
			if wait *= 2; wait > x.s.MaxBackoff {
				wait = x.s.MaxBackoff
			}
		}
		status, err := x.post(device, body)
		switch {
		case err != nil:
			last = err
		case status >= 200 && status < 300:
			return nil
		case retryable(status):
			last = fmt.Errorf("status %d", status)
		default:
			return fmt.Errorf("status %d, not retried", status)
		}
	}
	return fmt.Errorf("gave up after %d tries: %w", x.s.MaxRetries+1, last)
}

func (x *sender) post(device string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, x.s.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "infinimesh-webhook-addon")
	req.Header.Set(deviceHeader, device)
	if x.s.Secret != "" {
		req.Header.Set(signatureHeader, sign(x.s.Secret, body))
	}
	res, err := x.client.Do(req)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	res.Body.Close()
	return res.StatusCode, nil
}
