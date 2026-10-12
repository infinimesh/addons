package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/infinimesh/addons/sdk/config"
)

func TestSign(t *testing.T) {
	// echo -n '{"a":1}' | openssl dgst -sha256 -hmac secret
	if got := sign("secret", []byte(`{"a":1}`)); got != "sha256=aa9e2e3575f5d7098b6caccd790888c36d5fdb63342a73bada2d6a51747a8494" {
		t.Errorf("sign = %s", got)
	}
}

func testSender(url string, retries int) *sender {
	x := newSender(&settings{URL: url, Secret: "secret", Timeout: time.Second, MaxRetries: retries, Backoff: time.Millisecond, MaxBackoff: time.Millisecond})
	x.sleep = func(time.Duration) {}
	return x
}

func TestSendRetriesThenWorks(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get(signatureHeader) != sign("secret", body) || r.Header.Get(deviceHeader) != "d1" {
			t.Errorf("bad headers: %v", r.Header)
		}
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	if err := testSender(srv.URL, 5).send("d1", []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestSendStopsOn4xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	if err := testSender(srv.URL, 5).send("d1", []byte(`{}`)); err == nil || calls != 1 {
		t.Errorf("err = %v, calls = %d", err, calls)
	}
}

func TestSendGivesUp(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if err := testSender(srv.URL, 2).send("d1", []byte(`{}`)); err == nil || calls != 3 {
		t.Errorf("err = %v, calls = %d", err, calls)
	}
}

func TestLoadSettings(t *testing.T) {
	old := config.Env
	t.Cleanup(func() { config.Env = old })
	config.Env = func(k string) string { return map[string]string{"WEBHOOK_URL": "ftp://x"}[k] }
	if _, err := loadSettings(); err == nil {
		t.Error("want error for ftp URL")
	}
	config.Env = func(k string) string { return map[string]string{"WEBHOOK_URL": "https://example.com/hook"}[k] }
	s, err := loadSettings()
	if err != nil || s.Timeout != 10*time.Second || s.MaxRetries != 5 || s.Backoff != 500*time.Millisecond {
		t.Errorf("settings = %+v, err = %v", s, err)
	}
}
