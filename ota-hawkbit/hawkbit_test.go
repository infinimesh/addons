package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/infinimesh/addons/internal/otacore"
	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestActionStatusMatchesImage(t *testing.T) {
	if Image != "hawkbit/hawkbit-update-server:0.9.0" {
		t.Fatalf("image = %s", Image)
	}
	want := map[string]otacore.Mapped{
		"FINISHED":              {Status: "success"},
		"ERROR":                 {Status: "failed"},
		"WARNING":               {Status: "installing"},
		"RUNNING":               {Status: "installing"},
		"CANCELED":              {Status: "idle", Error: "canceled"},
		"CANCELING":             {Status: "idle", Error: "canceled"},
		"RETRIEVED":             {Status: "downloading"},
		"DOWNLOAD":              {Status: "downloading"},
		"SCHEDULED":             {Status: "idle"},
		"CANCEL_REJECTED":       {Status: "installing"},
		"DOWNLOADED":            {Status: "installing"},
		"WAIT_FOR_CONFIRMATION": {Status: "idle"},
	}
	if len(ActionStatus) != len(want) {
		t.Fatalf("status table has %d entries", len(ActionStatus))
	}
	for name, mapped := range want {
		got, ok := ActionStatus[name]
		if !ok || got != mapped {
			t.Fatalf("%s = %+v, %v", name, got, ok)
		}
		if _, ok := otacore.MapStatus(ActionStatus, name); !ok {
			t.Fatalf("%s did not map", name)
		}
		if _, ok := otacore.MapStatus(ActionStatus, strings.ToLower(name)); !ok {
			t.Fatalf("lowercase %s did not map", name)
		}
	}
}

func TestPollInterval(t *testing.T) {
	d, err := pollInterval("")
	if err != nil || d != 15*time.Second {
		t.Fatalf("default = %s %v", d, err)
	}
	d, err = pollInterval("0")
	if err != nil || d != 0 {
		t.Fatalf("zero = %s %v", d, err)
	}
	if _, err := pollInterval("1.5"); err == nil {
		t.Fatal("fraction accepted")
	}
	if _, err := pollInterval("15s"); err == nil {
		t.Fatal("suffix accepted")
	}
}

func TestRetrySendsTheSameBody(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c, err := newClient(srv.URL, "admin", "s3cret", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(context.Context, time.Duration) error { return nil }
	code, _, err := c.do(context.Background(), http.MethodPost, "/rest/v1/targets", []map[string]string{{
		"controllerId": "dev",
		"name":         "pump",
	}})
	if err != nil || code != http.StatusCreated {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if len(bodies) != 2 || bodies[0] == "" || bodies[0] != bodies[1] {
		t.Fatalf("bodies = %#v", bodies)
	}
	if strings.Contains(bodies[0], "s3cret") {
		t.Fatal("body contains the password")
	}
}

func TestPasswordStaysOutOfErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	c, err := newClient(srv.URL, "admin", "s3cret", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = c.ensureTarget(context.Background(), "dev", "pump")
	if err == nil || strings.Contains(err.Error(), "s3cret") || strings.Contains(err.Error(), "admin") {
		t.Fatalf("err = %v", err)
	}
}

func TestGroupsAndOneAction(t *testing.T) {
	h := newFakeHawkbit("s3cret")
	srv := httptest.NewServer(h)
	defer srv.Close()
	c, err := newClient(srv.URL, "admin", "s3cret", []string{"canary"})
	if err != nil {
		t.Fatal(err)
	}
	b := &backend{client: c}
	d := otacore.Device{ID: "dev-1", Namespace: "plant", Title: "pump", Tags: []string{"canary", "other"}}
	ctx := context.Background()
	if err := b.EnsureDevice(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := b.SyncGroup(ctx, d, []string{"canary", "other"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := b.SyncGroup(ctx, d, nil, []string{"other"}); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	if _, ok := h.tags["tag-other"]; ok {
		t.Fatal("tag-other was created")
	}
	if !h.members["ns-plant"]["dev-1"] || !h.members["tag-canary"]["dev-1"] {
		t.Fatalf("members = %#v", h.members)
	}
	h.mu.Unlock()

	for name := range ActionStatus {
		h.setAction("dev-1", name, 7)
		snap, err := b.Status(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		if snap.RawStatus != name || snap.Version != "1.2.0" {
			t.Fatalf("%s snap = %+v", name, snap)
		}
		got, ok := otacore.MapStatus(ActionStatus, snap.RawStatus)
		if !ok || got != ActionStatus[name] {
			t.Fatalf("%s mapped %+v", name, got)
		}
	}
	h.mu.Lock()
	delete(h.actions, "dev-1")
	h.mu.Unlock()

	dir := t.TempDir()
	w := &memWriter{}
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(osStderr) })
	r := otacore.Runner{
		Settings: otacore.Settings{
			Token:      "addon-token",
			Namespaces: []string{"plant"},
			StateFile:  filepath.Join(dir, "state.json"),
			APIURL:     "https://api.example.com",
		},
		Backend: b,
		Writer:  w,
		Status:  ActionStatus,
		Now:     func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) },
	}
	added := &shadowpb.Shadow{Device: "dev-1", Title: "pump", Event: shadowpb.ShadowEvent_DEVICE_ADDED, Tags: []string{"canary"}}
	req := requestedShadow("dev-1", "1.2.0")
	r.Open = shadows(added, req)
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.assignPosts() != 1 {
		t.Fatalf("actions = %d", h.assignPosts())
	}
	h.mu.Lock()
	posted := h.lastAssign
	h.mu.Unlock()
	if !strings.Contains(posted, `"type":"forced"`) {
		t.Fatalf("assign body = %s", posted)
	}
	r.Open = shadows(added, req)
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.assignPosts() != 1 {
		t.Fatalf("restart actions = %d", h.assignPosts())
	}
	h.setAction("dev-1", "CANCELED", 7)
	r.Open = shadows(&shadowpb.Shadow{Device: "dev-1", Event: shadowpb.ShadowEvent_STATE})
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.Open = shadows(requestedShadow("dev-1", "1.2.0"))
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, p := range w.patches {
		if p.Reported["status"] == "idle" && p.Reported["error"] == "canceled" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("patches = %#v", w.patches)
	}
	before := len(w.patches)
	r.Open = shadows(requestedShadow("dev-1", "missing"))
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.assignPosts() != 1 {
		t.Fatalf("unknown version posted an action")
	}
	if len(w.patches) != before+1 || w.patches[before].Reported["status"] != "failed" {
		t.Fatalf("fail patch = %#v", w.patches[before:])
	}
	if !strings.Contains(w.patches[before].Reported["error"].(string), "unknown version missing") {
		t.Fatalf("error = %v", w.patches[before].Reported["error"])
	}
	r.Open = shadows(requestedShadow("dev-1", "missing"))
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.patches) != before+1 {
		t.Fatalf("unknown version was retried, patches %d", len(w.patches))
	}
	if strings.Contains(buf.String(), "s3cret") || strings.Contains(buf.String(), "addon-token") {
		t.Fatalf("log = %s", buf.String())
	}
}

func TestRetryAfterAndCancel(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c, err := newClient(srv.URL, "admin", "s3cret", nil)
	if err != nil {
		t.Fatal(err)
	}
	var waits []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	code, _, err := c.do(context.Background(), http.MethodPost, "/rest/v1/targets", []map[string]string{{"controllerId": "dev", "name": "pump"}})
	if err != nil || code != http.StatusCreated {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if len(waits) != 1 || waits[0] < 3*time.Second || waits[0] >= 4*time.Second {
		t.Fatalf("waits = %v", waits)
	}
	n = 0
	c.sleep = func(context.Context, time.Duration) error { return context.Canceled }
	_, _, err = c.do(context.Background(), http.MethodGet, "/rest/v1/targets/dev", nil)
	if !errors.Is(err, context.Canceled) || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestOutageStaysUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	c, err := newClient(url, "admin", "s3cret", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(context.Context, time.Duration) error { return nil }
	dir := t.TempDir()
	w := &memWriter{}
	r := otacore.Runner{
		Settings: otacore.Settings{
			Token: "addon-token", Namespaces: []string{"plant"},
			StateFile: filepath.Join(dir, "state.json"), APIURL: "https://api.example",
		},
		Backend: &backend{client: c},
		Writer:  w,
		Open:    shadows(&shadowpb.Shadow{Device: "dev-1", Event: shadowpb.ShadowEvent_DEVICE_ADDED}),
		Status:  ActionStatus,
		Now:     func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) },
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.patches) != 1 || w.patches[0].Reported["error"] != "hawkbit unreachable" {
		t.Fatalf("patches = %#v", w.patches)
	}
	if strings.Contains(fmtSprint(w.patches), "s3cret") {
		t.Fatal("patch contains the password")
	}
}

func fmtSprint(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestUnreachableReportsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c, err := newClient(url, "admin", "s3cret", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(context.Context, time.Duration) error { return nil }
	snap, err := (&backend{client: c}).Status(context.Background(), otacore.Device{ID: "dev"})
	if err != nil || snap.Error != "hawkbit unreachable" {
		t.Fatalf("snap=%+v err=%v", snap, err)
	}
}

func TestNoAssignmentAndDetailStatus(t *testing.T) {
	mode := "empty"
	var posts, fetches int
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/assignedDS") && r.Method == http.MethodGet:
			if mode == "empty" || mode == "blank" {
				if mode == "blank" {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"id": 7, "name": "1.2.0"})
		case strings.HasSuffix(p, "/actions") && r.Method == http.MethodGet:
			if mode == "empty" || mode == "blank" {
				writeJSON(w, http.StatusOK, map[string]any{"content": []any{}})
				return
			}
			row := map[string]any{"id": 4, "status": "pending", "detailStatus": "running"}
			if mode == "link" {
				row["_links"] = map[string]any{"distributionset": map[string]any{"href": srv.URL + "/rest/v1/distributionsets/7"}}
			}
			if mode == "finished" {
				row = map[string]any{"id": 4, "status": "finished"}
			}
			writeJSON(w, http.StatusOK, map[string]any{"content": []any{row}})
		case strings.HasSuffix(p, "/actions/4") && r.Method == http.MethodGet:
			fetches++
			writeJSON(w, http.StatusOK, map[string]any{
				"id": 4, "detailStatus": "running",
				"_links": map[string]any{"distributionset": map[string]any{"href": srv.URL + "/rest/v1/distributionsets/7"}},
			})
		case strings.Contains(p, "/distributionsets") && r.Method == http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{"content": []any{map[string]any{"id": 7.0, "name": "1.2.0"}}})
		case strings.HasSuffix(p, "/assignedDS") && r.Method == http.MethodPost:
			posts++
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c, err := newClient(srv.URL, "admin", "s3cret", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(context.Context, time.Duration) error { return nil }
	b := &backend{client: c}
	d := otacore.Device{ID: "dev", Namespace: "plant"}
	snap, err := b.Status(context.Background(), d)
	if err != nil || snap.Error != "" || snap.RawStatus != "SCHEDULED" {
		t.Fatalf("empty snap=%+v err=%v", snap, err)
	}
	mode = "blank"
	snap, err = b.Status(context.Background(), d)
	if err != nil || snap.Error != "" {
		t.Fatalf("blank snap=%+v err=%v", snap, err)
	}
	mode = "link"
	snap, err = b.Status(context.Background(), d)
	if err != nil || snap.RawStatus != "running" || snap.Error != "" {
		t.Fatalf("link snap=%+v err=%v", snap, err)
	}
	if _, ok := otacore.MapStatus(ActionStatus, snap.RawStatus); !ok {
		t.Fatal("running did not map")
	}
	if err := b.Deploy(context.Background(), d, "1.2.0"); err != nil || posts != 0 {
		t.Fatalf("deploy err=%v posts=%d", err, posts)
	}
	mode = "fetch"
	if err := b.Deploy(context.Background(), d, "1.2.0"); err != nil || posts != 0 || fetches != 1 {
		t.Fatalf("fetch deploy err=%v posts=%d fetches=%d", err, posts, fetches)
	}
	mode = "finished"
	snap, err = b.Status(context.Background(), d)
	if err != nil || snap.RawStatus != "finished" {
		t.Fatalf("finished snap=%+v err=%v", snap, err)
	}
	got, ok := otacore.MapStatus(ActionStatus, snap.RawStatus)
	if !ok || got.Status != "success" {
		t.Fatalf("finished mapped %+v %v", got, ok)
	}
}

func TestSkipDeployType(t *testing.T) {
	h := newFakeHawkbit("s3cret")
	srv := httptest.NewServer(h)
	defer srv.Close()
	c, _ := newClient(srv.URL, "admin", "s3cret", nil)
	err := (&backend{client: c}).Deploy(context.Background(), otacore.Device{ID: "dev"}, "nope")
	var skip *otacore.SkipDeploy
	if !errors.As(err, &skip) || skip.Error() != "unknown version nope" {
		t.Fatalf("err = %v", err)
	}
}

type fakeHawkbit struct {
	mu         sync.Mutex
	pass       string
	targets    map[string]string
	tags       map[string]float64
	members    map[string]map[string]bool
	nextTag    float64
	actions    map[string]action
	assigns    int
	lastAssign string
}

func newFakeHawkbit(pass string) *fakeHawkbit {
	return &fakeHawkbit{
		pass:    pass,
		targets: map[string]string{},
		tags:    map[string]float64{},
		members: map[string]map[string]bool{},
		actions: map[string]action{},
		nextTag: 1,
	}
}

func (h *fakeHawkbit) assignPosts() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.assigns
}

func (h *fakeHawkbit) setAction(controller, status string, distID float64) {
	h.mu.Lock()
	h.actions[controller] = action{Status: status, DistID: distID}
	h.mu.Unlock()
}

func (h *fakeHawkbit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, pass, ok := r.BasicAuth()
	if !ok || user != "admin" || pass != h.pass {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case len(parts) == 3 && parts[2] == "targets" && r.Method == http.MethodPost:
		var rows []map[string]string
		_ = json.Unmarshal(raw, &rows)
		for _, row := range rows {
			h.targets[row["controllerId"]] = row["name"]
		}
		writeJSON(w, http.StatusCreated, rows)
	case len(parts) == 4 && parts[2] == "targets" && r.Method == http.MethodGet:
		name, ok := h.targets[parts[3]]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"controllerId": parts[3], "name": name})
	case len(parts) == 4 && parts[2] == "targets" && r.Method == http.MethodDelete:
		delete(h.targets, parts[3])
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 5 && parts[2] == "targets" && parts[4] == "assignedDS" && r.Method == http.MethodGet:
		act, ok := h.actions[parts[3]]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": act.DistID, "name": "1.2.0"})
	case len(parts) == 5 && parts[2] == "targets" && parts[4] == "assignedDS" && r.Method == http.MethodPost:
		h.assigns++
		h.lastAssign = string(raw)
		var rows []map[string]any
		_ = json.Unmarshal(raw, &rows)
		if len(rows) != 1 || rows[0]["type"] != "forced" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id, _ := rows[0]["id"].(float64)
		h.actions[parts[3]] = action{Status: "RUNNING", DistID: id}
		writeJSON(w, http.StatusOK, rows)
	case len(parts) == 5 && parts[2] == "targets" && parts[4] == "actions" && r.Method == http.MethodGet:
		act, ok := h.actions[parts[3]]
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"content": []any{}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"content": []any{
			map[string]any{"status": act.Status, "distributionSet": map[string]any{"id": act.DistID}},
		}})
	case len(parts) == 3 && parts[2] == "targettags" && r.Method == http.MethodGet:
		name := strings.Trim(strings.TrimPrefix(r.URL.Query().Get("q"), "name=="), `"`)
		if id, ok := h.tags[name]; ok {
			writeJSON(w, http.StatusOK, map[string]any{"content": []any{map[string]any{"id": id, "name": name}}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"content": []any{}})
	case len(parts) == 3 && parts[2] == "targettags" && r.Method == http.MethodPost:
		var rows []map[string]string
		_ = json.Unmarshal(raw, &rows)
		out := []map[string]any{}
		for _, row := range rows {
			h.nextTag++
			h.tags[row["name"]] = h.nextTag
			out = append(out, map[string]any{"id": h.nextTag, "name": row["name"]})
		}
		writeJSON(w, http.StatusCreated, out)
	case len(parts) == 6 && parts[2] == "targettags" && parts[4] == "assigned":
		var id float64
		for name, tagID := range h.tags {
			if strconv.FormatFloat(tagID, 'f', 0, 64) == parts[3] {
				if h.members[name] == nil {
					h.members[name] = map[string]bool{}
				}
				if r.Method == http.MethodDelete {
					delete(h.members[name], parts[5])
				} else {
					h.members[name][parts[5]] = true
				}
				id = tagID
			}
		}
		if id == 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	case len(parts) == 3 && parts[2] == "distributionsets" && r.Method == http.MethodGet:
		name := strings.Trim(strings.TrimPrefix(r.URL.Query().Get("q"), "name=="), `"`)
		if name == "1.2.0" {
			writeJSON(w, http.StatusOK, map[string]any{"content": []any{map[string]any{"id": 7.0, "name": "1.2.0"}}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"content": []any{}})
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

func shadows(msgs ...*shadowpb.Shadow) otacore.StreamOpen {
	return func(ctx context.Context, _ string, _ uint64) (<-chan *shadowpb.Shadow, <-chan error) {
		ch := make(chan *shadowpb.Shadow)
		errc := make(chan error)
		go func() {
			defer close(errc)
			defer close(ch)
			for _, msg := range msgs {
				select {
				case <-ctx.Done():
					return
				case ch <- msg:
				}
			}
		}()
		return ch, errc
	}
}

func requestedShadow(device, version string) *shadowpb.Shadow {
	ota, err := structpb.NewStruct(map[string]any{"requested_version": version})
	if err != nil {
		panic(err)
	}
	return &shadowpb.Shadow{
		Device: device,
		Event:  shadowpb.ShadowEvent_OTA_REQUESTED,
		Desired: &shadowpb.State{Data: &structpb.Struct{Fields: map[string]*structpb.Value{
			"ota": structpb.NewStructValue(ota),
		}}},
	}
}

var osStderr = log.Writer()
