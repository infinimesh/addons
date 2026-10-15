package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/infinimesh/addons/internal/otacore"
	shadowpb "github.com/infinimesh/proto/shadow"
)

func TestPickGroupKeepsNamespace(t *testing.T) {
	g, warn := pickGroup("plant", "", []string{"canary"})
	if g != "plant" || warn {
		t.Fatalf("group=%s warn=%v", g, warn)
	}
	g, warn = pickGroup("plant", "canary", nil)
	if g != "plant" || warn {
		t.Fatalf("group=%s warn=%v", g, warn)
	}
	g, warn = pickGroup("plant", "canary", []string{"canary"})
	if g != "plant" || !warn {
		t.Fatalf("group=%s warn=%v", g, warn)
	}
}

func TestDeviceStatusTable(t *testing.T) {
	cases := []struct {
		raw, status, err string
	}{
		{"pending", "idle", ""},
		{"downloading", "downloading", ""},
		{"installing", "installing", ""},
		{"rebooting", "installing", ""},
		{"success", "success", ""},
		{"already-installed", "success", ""},
		{"failure", "failed", ""},
		{"aborted", "failed", ""},
		{"noartifact", "failed", ""},
		{"UNREGISTERED", "idle", "not registered in mender"},
	}
	for _, tc := range cases {
		m, ok := otacore.MapStatus(DeviceStatus, tc.raw)
		if !ok || m.Status != tc.status || m.Error != tc.err {
			t.Fatalf("%s -> %+v ok=%v", tc.raw, m, ok)
		}
	}
}

func TestDeployOnceAndUnknownVersion(t *testing.T) {
	h := &fakeMender{devices: map[string]string{"dev-1": "m-1"}, artifacts: map[string]bool{"1.2.0": true}}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c, err := newClient(srv.URL, "", "", "tok", "infinimesh_id")
	if err != nil {
		t.Fatal(err)
	}
	b := newBackend(c, "canary")
	d := otacore.Device{ID: "dev-1", Namespace: "plant", Tags: []string{"canary"}}
	var logs bytesLog
	prev := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(prev)
	if err := b.EnsureDevice(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if h.group["m-1"] != "plant" {
		t.Fatalf("group=%s", h.group["m-1"])
	}
	if !strings.Contains(logs.String(), "keeping namespace group plant") {
		t.Fatalf("log=%q", logs.String())
	}
	if err := b.Deploy(context.Background(), d, "1.2.0"); err != nil {
		t.Fatal(err)
	}
	if err := b.Deploy(context.Background(), d, "1.2.0"); err != nil {
		t.Fatal(err)
	}
	if h.posts != 1 {
		t.Fatalf("posts=%d", h.posts)
	}
	err = b.Deploy(context.Background(), d, "missing")
	var skip *otacore.SkipDeploy
	if !errors.As(err, &skip) {
		t.Fatalf("err=%v", err)
	}
	if h.posts != 1 {
		t.Fatalf("posts=%d", h.posts)
	}
	snap, err := b.Status(context.Background(), d)
	if err != nil || snap.Version != "1.2.0" || snap.RawStatus != "pending" {
		t.Fatalf("snap=%+v err=%v", snap, err)
	}
}

func TestRejectedToken(t *testing.T) {
	var logs bytesLog
	prev := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(prev)
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		h := &fakeMender{auth: code}
		srv := httptest.NewServer(h)
		c, err := newClient(srv.URL, "", "", "tok", "infinimesh_id")
		if err != nil {
			t.Fatal(err)
		}
		b := newBackend(c, "")
		err = b.EnsureDevice(context.Background(), otacore.Device{ID: "dev-1", Namespace: "plant"})
		srv.Close()
		var down *otacore.Unavailable
		if !errors.As(err, &down) || down.Error() != "mender token rejected" {
			t.Fatalf("code %d err=%v", code, err)
		}
	}
	if strings.Count(logs.String(), "mender rejected the token") != 1 {
		t.Fatalf("log=%q", logs.String())
	}
}

func TestReassignOlderAbortsPending(t *testing.T) {
	h := &fakeMender{
		devices:   map[string]string{"dev-1": "m-1"},
		artifacts: map[string]bool{"fw-1.0.0": true, "fw-2.0.0": true},
		history: []map[string]any{{
			"deployment": map[string]string{"id": "old", "artifact_name": "fw-1.0.0", "created": "2026-10-15T00:00:00Z"},
			"device":     map[string]string{"status": "success"},
		}},
		open: []fakeDep{{
			ID: "new", Artifact: "fw-2.0.0", Status: "pending", Created: "2026-10-15T00:00:01Z", Devices: []string{"m-1"},
		}},
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c, err := newClient(srv.URL, "", "", "tok", "infinimesh_id")
	if err != nil {
		t.Fatal(err)
	}
	b := newBackend(c, "")
	d := otacore.Device{ID: "dev-1", Namespace: "plant"}
	if err := b.EnsureDevice(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	snap, err := b.Status(context.Background(), d)
	if err != nil || snap.Version != "fw-2.0.0" || snap.RawStatus != "pending" {
		t.Fatalf("before snap=%+v err=%v", snap, err)
	}
	if err := b.Deploy(context.Background(), d, "fw-1.0.0"); err != nil {
		t.Fatal(err)
	}
	if h.aborts != 1 || h.posts != 1 {
		t.Fatalf("aborts=%d posts=%d", h.aborts, h.posts)
	}
	snap, err = b.Status(context.Background(), d)
	if err != nil || snap.Version != "fw-1.0.0" || snap.RawStatus != "pending" {
		t.Fatalf("after snap=%+v err=%v", snap, err)
	}
}

func TestUnregisteredIsIdle(t *testing.T) {
	h := &fakeMender{devices: map[string]string{}, artifacts: map[string]bool{}}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c, err := newClient(srv.URL, "", "", "tok", "infinimesh_id")
	if err != nil {
		t.Fatal(err)
	}
	w := &memWriter{}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err = (&otacore.Runner{
		Settings: otacore.Settings{Token: "t", Namespaces: []string{"plant"}, APIURL: "https://api.example", StateFile: t.TempDir() + "/state.json"},
		Backend:  newBackend(c, ""),
		Open:     oneShadow(&shadowpb.Shadow{Device: "dev-1", Event: shadowpb.ShadowEvent_DEVICE_ADDED}),
		Writer:   w,
		Status:   DeviceStatus,
		Poll:     time.Hour,
		Now:      func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) },
	}).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.patches) == 0 {
		t.Fatal("no patch")
	}
	p := w.patches[0]
	if p.Reported["status"] != "idle" || p.Reported["error"] != notRegistered {
		t.Fatalf("patch=%v", p.Reported)
	}
}

func TestStatusPhases(t *testing.T) {
	h := &fakeMender{
		devices:   map[string]string{"dev-1": "m-1"},
		artifacts: map[string]bool{"1.2.0": true},
		status:    "rebooting",
		created:   "2026-10-15T00:00:01Z",
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c, _ := newClient(srv.URL, "", "", "tok", "")
	b := newBackend(c, "")
	d := otacore.Device{ID: "dev-1", Namespace: "plant"}
	if err := b.EnsureDevice(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if err := b.Deploy(context.Background(), d, "1.2.0"); err != nil {
		t.Fatal(err)
	}
	h.status = "rebooting"
	snap, err := b.Status(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := otacore.MapStatus(DeviceStatus, snap.RawStatus)
	if !ok || m.Status != "installing" {
		t.Fatalf("snap=%+v mapped=%+v", snap, m)
	}
	h.status = "already-installed"
	snap, err = b.Status(context.Background(), d)
	if err != nil || snap.ReportedVersion != "1.2.0" {
		t.Fatalf("snap=%+v err=%v", snap, err)
	}
	if err := b.Forget(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if h.group["m-1"] != "" {
		t.Fatalf("group still %s", h.group["m-1"])
	}
}

type bytesLog struct{ b strings.Builder }

func (l *bytesLog) Write(p []byte) (int, error) { return l.b.Write(p) }
func (l *bytesLog) String() string              { return l.b.String() }

type fakeDep struct {
	ID, Artifact, Status, Created string
	Devices                       []string
}

type fakeMender struct {
	mu        sync.Mutex
	devices   map[string]string
	artifacts map[string]bool
	group     map[string]string
	posts     int
	aborts    int
	status    string
	created   string
	auth      int
	history   []map[string]any
	open      []fakeDep
}

func (h *fakeMender) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.group == nil {
		h.group = map[string]string{}
	}
	if h.auth == http.StatusUnauthorized || h.auth == http.StatusForbidden {
		w.WriteHeader(h.auth)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	switch {
	case r.URL.Path == "/api/management/v1/inventory/devices" && r.Method == http.MethodGet:
		id := h.devices[r.URL.Query().Get("infinimesh_id")]
		if id == "" {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeJSON(w, http.StatusOK, []map[string]string{{"id": id}})
	case strings.HasPrefix(r.URL.Path, "/api/management/v1/inventory/devices/") && strings.HasSuffix(r.URL.Path, "/group") && r.Method == http.MethodPut:
		var body struct {
			Group string `json:"group"`
		}
		_ = json.Unmarshal(raw, &body)
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/management/v1/inventory/devices/"), "/group")
		h.group[id] = body.Group
		w.WriteHeader(http.StatusNoContent)
	case strings.Contains(r.URL.Path, "/group/") && r.Method == http.MethodDelete:
		id := strings.TrimPrefix(r.URL.Path, "/api/management/v1/inventory/devices/")
		id = id[:strings.Index(id, "/group/")]
		delete(h.group, id)
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/api/management/v1/deployments/deployments/releases" && r.Method == http.MethodGet:
		name := r.URL.Query().Get("name")
		if h.artifacts[name] {
			writeJSON(w, http.StatusOK, []map[string]string{{"name": name}})
			return
		}
		writeJSON(w, http.StatusOK, []any{})
	case strings.HasPrefix(r.URL.Path, "/api/management/v1/deployments/deployments/devices/") && r.Method == http.MethodGet:
		if h.history != nil {
			writeJSON(w, http.StatusOK, h.history)
			return
		}
		if h.posts == 0 || h.status == "" {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		created := h.created
		if created == "" {
			created = "2026-10-15T00:00:00Z"
		}
		writeJSON(w, http.StatusOK, []any{map[string]any{
			"deployment": map[string]string{"id": "posted-1", "artifact_name": "1.2.0", "created": created},
			"device":     map[string]string{"status": h.status},
		}})
	case r.URL.Path == "/api/management/v1/deployments/deployments" && r.Method == http.MethodGet:
		status := r.URL.Query().Get("status")
		var rows []map[string]string
		for _, dep := range h.open {
			if dep.Status == status {
				rows = append(rows, map[string]string{
					"id": dep.ID, "artifact_name": dep.Artifact, "created": dep.Created, "status": dep.Status,
				})
			}
		}
		writeJSON(w, http.StatusOK, rows)
	case strings.HasSuffix(r.URL.Path, "/device_list") && r.Method == http.MethodGet:
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/management/v1/deployments/deployments/"), "/device_list")
		for _, dep := range h.open {
			if dep.ID == id {
				writeJSON(w, http.StatusOK, dep.Devices)
				return
			}
		}
		writeJSON(w, http.StatusOK, []string{})
	case strings.HasSuffix(r.URL.Path, "/status") && r.Method == http.MethodPut:
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/management/v1/deployments/deployments/"), "/status")
		h.aborts++
		var left []fakeDep
		for _, dep := range h.open {
			if dep.ID != id {
				left = append(left, dep)
			}
		}
		h.open = left
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/api/management/v1/deployments/deployments" && r.Method == http.MethodPost:
		var body struct {
			Artifact string   `json:"artifact_name"`
			Devices  []string `json:"devices"`
		}
		_ = json.Unmarshal(raw, &body)
		h.posts++
		h.open = append(h.open, fakeDep{
			ID: "posted-" + strconv.Itoa(h.posts), Artifact: body.Artifact, Status: "pending",
			Created: "2026-10-15T00:00:02Z", Devices: body.Devices,
		})
		w.WriteHeader(http.StatusCreated)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type memWriter struct {
	mu      sync.Mutex
	patches []otacore.Patch
}

func (m *memWriter) Patch(_ context.Context, p otacore.Patch) error {
	m.mu.Lock()
	m.patches = append(m.patches, p)
	m.mu.Unlock()
	return nil
}

func oneShadow(msg *shadowpb.Shadow) otacore.StreamOpen {
	return func(ctx context.Context, _ string, _ uint64) (<-chan *shadowpb.Shadow, <-chan error) {
		ch := make(chan *shadowpb.Shadow)
		errc := make(chan error)
		go func() {
			defer close(errc)
			defer close(ch)
			select {
			case <-ctx.Done():
			case ch <- msg:
			}
		}()
		return ch, errc
	}
}
