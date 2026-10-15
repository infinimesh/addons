package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/infinimesh/addons/internal/otacore"
	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestBadSignatureWritesNothing(t *testing.T) {
	w := &fakeWriter{}
	a := testApp(t, w)
	body := []byte(`{"device":"pump","target":{"version":"1.2.0"}}`)
	rec := post(a, body, "sha256=00")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if len(w.patches) != 0 {
		t.Fatalf("writer called: %+v", w.patches)
	}
	rec = post(a, body, "")
	if rec.Code != http.StatusUnauthorized || len(w.patches) != 0 {
		t.Fatalf("missing signature status %d patches %d", rec.Code, len(w.patches))
	}
}

func TestValidTargetWritesDesired(t *testing.T) {
	w := &fakeWriter{}
	a := testApp(t, w)
	a.now = func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) }
	sum := strings.Repeat("ab", 32)
	body := []byte(`{"device":"pump","target":{"version":"1.2.0","url":"https://files.example/a","sha256":"` + sum + `","size":4}}`)
	rec := post(a, body, sign("secret", body))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if len(w.patches) != 1 {
		t.Fatalf("patches %d", len(w.patches))
	}
	p := w.patches[0]
	if p.Device != "pump" || p.Desired["version"] != "1.2.0" || p.Desired["url"] != "https://files.example/a" || p.Desired["sha256"] != sum || p.Desired["size"] != float64(4) {
		t.Fatalf("desired %+v", p.Desired)
	}
	if p.Desired["updated_at"] != "2026-10-15T00:00:00Z" {
		t.Fatalf("updated_at %v", p.Desired["updated_at"])
	}
	if _, ok := p.Desired["requested_version"]; ok || p.Reported != nil {
		t.Fatalf("patch %+v", p)
	}
}

func TestReportWritesStatus(t *testing.T) {
	w := &fakeWriter{}
	a := testApp(t, w)
	body := []byte(`{"device":"pump","report":{"status":"installing","error":"disk"}}`)
	rec := post(a, body, sign("secret", body))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	p := w.patches[0]
	if p.Reported["status"] != "installing" || p.Reported["error"] != "disk" || p.Desired != nil {
		t.Fatalf("reported %+v", p.Reported)
	}
}

func TestRequestedVersionAndUnknownKey(t *testing.T) {
	w := &fakeWriter{}
	a := testApp(t, w)
	cases := []struct {
		body string
		msg  string
	}{
		{`{"device":"pump","target":{"requested_version":"1.4.0"}}`, "addon token can write desired ota only"},
		{`{"device":"pump","target":{"progress":"1"}}`, "ota field progress is not supported"},
		{`{"device":"pump","report":{"status":"pending"}}`, "ota status must be idle, downloading, installing, success, or failed"},
		{`{"device":"pump","target":{"sha256":"ABCD"}}`, "ota sha256 must be 64 lowercase hex characters"},
		{`{"device":"pump","target":{"size":1.5}}`, "ota size must be a whole number of zero or greater"},
		{`{"device":"pump","target":{"version":1}}`, "ota version must be a string"},
	}
	for _, tc := range cases {
		body := []byte(tc.body)
		rec := post(a, body, sign("secret", body))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.msg) {
			t.Fatalf("%s: status %d body %s", tc.msg, rec.Code, rec.Body.String())
		}
	}
	if len(w.patches) != 0 {
		t.Fatalf("writer called: %+v", w.patches)
	}
}

func TestEventOnceAndRetry(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	fail := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Header.Get(signatureHeader) != sign("secret", b) {
			t.Errorf("signature %s", r.Header.Get(signatureHeader))
		}
		mu.Lock()
		bodies = append(bodies, string(b))
		first := fail
		fail = false
		mu.Unlock()
		if first {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	a := testApp(t, &fakeWriter{})
	a.urls = map[string]string{"plant": srv.URL}
	a.post.sleep = func(context.Context, time.Duration) error { return nil }
	msg := otaEvent(shadowpb.ShadowEvent_OTA_REQUESTED, 9, "1.4.0")
	if err := a.deliver(context.Background(), "plant", msg); err != nil {
		t.Fatal(err)
	}
	if err := a.deliver(context.Background(), "plant", msg); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("posts %d", len(bodies))
	}
	if bodies[0] != bodies[1] {
		t.Fatalf("retry body changed\n%s\n%s", bodies[0], bodies[1])
	}
	var doc outbound
	if err := json.Unmarshal([]byte(bodies[0]), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.EventID != 9 || doc.Event != "OTA_REQUESTED" || doc.Desired["requested_version"] != "1.4.0" {
		t.Fatalf("doc %+v", doc)
	}
}

func TestFiveXXDoesNotDuplicateAfterSuccess(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	a := testApp(t, &fakeWriter{})
	a.urls = map[string]string{"plant": srv.URL}
	a.post.maxRetries = 3
	a.post.sleep = func(context.Context, time.Duration) error { return nil }
	msg := &shadowpb.Shadow{Device: "pump", EventId: 4, Event: shadowpb.ShadowEvent_OTA_STATUS_CHANGED}
	if err := a.handle(context.Background(), "plant", msg); err != nil {
		t.Fatal(err)
	}
	if err := a.handle(context.Background(), "plant", msg); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("posts %d", n)
	}
}

func TestRestartSkipsSentEvent(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	dir := t.TempDir()
	book, err := loadBook(dir + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(config{Secret: "secret", URLs: map[string]string{"plant": srv.URL}}, book, nil, &fakeWriter{})
	msg := &shadowpb.Shadow{Device: "pump", EventId: 11, Event: shadowpb.ShadowEvent_DEVICE_ADDED, Namespace: "plant"}
	if err := a.deliver(context.Background(), "plant", msg); err != nil {
		t.Fatal(err)
	}
	again, err := loadBook(dir + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	a.book = again
	if err := a.deliver(context.Background(), "plant", msg); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("posts %d", n)
	}
}

func TestForwardsMembershipAndOTAEvents(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var doc outbound
		if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
			t.Error(err)
		}
		got = append(got, doc.Event)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	a := testApp(t, &fakeWriter{})
	a.urls = map[string]string{"plant": srv.URL}
	events := []shadowpb.ShadowEvent{
		shadowpb.ShadowEvent_DEVICE_ADDED,
		shadowpb.ShadowEvent_DEVICE_REMOVED,
		shadowpb.ShadowEvent_TAG_ADDED,
		shadowpb.ShadowEvent_TAG_REMOVED,
		shadowpb.ShadowEvent_OTA_TARGET_SET,
		shadowpb.ShadowEvent_OTA_STATUS_CHANGED,
		shadowpb.ShadowEvent_OTA_REQUESTED,
		shadowpb.ShadowEvent_STATE,
	}
	for i, ev := range events {
		msg := &shadowpb.Shadow{Device: "pump", EventId: uint64(i + 1), Event: ev, Namespace: "plant"}
		if err := a.handle(context.Background(), "plant", msg); err != nil {
			t.Fatal(err)
		}
	}
	want := "DEVICE_ADDED,DEVICE_REMOVED,TAG_ADDED,TAG_REMOVED,OTA_TARGET_SET,OTA_STATUS_CHANGED,OTA_REQUESTED"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %s", strings.Join(got, ","))
	}
}

func TestWebhookURLRequired(t *testing.T) {
	env := map[string]string{
		"OTA_ADDON_TOKEN":    "tok",
		"OTA_NAMESPACE":      "plant",
		"OTA_STATE_FILE":     "/tmp/ota.json",
		"API_URL":            "https://api.example",
		"OTA_WEBHOOK_SECRET": "secret",
	}
	_, err := load(func(k string) string { return env[k] })
	if err == nil || err.Error() != "webhook_url is required when provider is webhook" {
		t.Fatalf("err %v", err)
	}
	env["OTA_WEBHOOK_URL"] = "http://hook.example/ota"
	_, err = load(func(k string) string { return env[k] })
	if err == nil || !strings.Contains(err.Error(), "OTA_ALLOW_INSECURE") {
		t.Fatalf("err %v", err)
	}
	env["OTA_ALLOW_INSECURE"] = "1"
	cfg, err := load(func(k string) string { return env[k] })
	if err != nil || cfg.URLs["plant"] != "http://hook.example/ota" {
		t.Fatalf("cfg %+v err %v", cfg.URLs, err)
	}
}

func testApp(t *testing.T, w otacore.Writer) *app {
	t.Helper()
	book, err := loadBook(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(config{Secret: "secret", URLs: map[string]string{"plant": "https://hook.example/ota"}, Listen: ":0"}, book, nil, w)
	a.now = func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) }
	return a
}

func post(a *app, body []byte, sig string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	if sig != "" {
		req.Header.Set(signatureHeader, sig)
	}
	rec := httptest.NewRecorder()
	a.serveHTTP(rec, req)
	return rec
}

func otaEvent(ev shadowpb.ShadowEvent, id uint64, requested string) *shadowpb.Shadow {
	ota, err := structpb.NewStruct(map[string]any{"requested_version": requested})
	if err != nil {
		panic(err)
	}
	data, err := structpb.NewStruct(map[string]any{})
	if err != nil {
		panic(err)
	}
	data.Fields["ota"] = structpb.NewStructValue(ota)
	return &shadowpb.Shadow{
		Device:    "pump",
		Namespace: "plant",
		Event:     ev,
		EventId:   id,
		Desired:   &shadowpb.State{Data: data},
	}
}

type fakeWriter struct {
	patches []otacore.Patch
}

func (f *fakeWriter) Patch(_ context.Context, p otacore.Patch) error {
	f.patches = append(f.patches, p)
	return nil
}
