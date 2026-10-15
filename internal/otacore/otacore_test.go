package otacore

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/infinimesh/proto/node"
	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestMapStatus(t *testing.T) {
	table := map[string]Mapped{
		"idle":        {Status: "idle"},
		"downloading": {Status: "downloading"},
		"installing":  {Status: "installing"},
		"success":     {Status: "success"},
		"failed":      {Status: "failed"},
		"canceled":    {Status: "idle", Error: "canceled"},
		"bogus":       {Status: "rebooting"},
	}
	for _, raw := range []string{"idle", "downloading", "installing", "success", "failed"} {
		got, ok := MapStatus(table, raw)
		if !ok || got.Status != raw || got.Error != "" {
			t.Fatalf("MapStatus(%s) = %+v, %v", raw, got, ok)
		}
	}
	got, ok := MapStatus(table, "canceled")
	if !ok || got.Status != "idle" || got.Error != "canceled" {
		t.Fatalf("canceled = %+v, %v", got, ok)
	}
	if _, ok := MapStatus(table, "bogus"); ok {
		t.Fatal("rebooting was accepted")
	}
	if _, ok := MapStatus(table, "missing"); ok {
		t.Fatal("missing was accepted")
	}
}

func TestIdempotencyKey(t *testing.T) {
	if IdempotencyKey("dev", "1.2.0") != IdempotencyKey("dev", "1.2.0") {
		t.Fatal("same deploy did not produce the same key")
	}
	if IdempotencyKey("dev", "1.2.0") == IdempotencyKey("dev", "1.2.1") {
		t.Fatal("version change reused a key")
	}
	if IdempotencyKey("dev", "1.2.0") == IdempotencyKey("other", "1.2.0") {
		t.Fatal("device change reused a key")
	}
}

func TestDiffGroups(t *testing.T) {
	add, remove := DiffGroups([]string{"b", "a"}, []string{"a", "c"})
	if strings.Join(add, ",") != "c" || strings.Join(remove, ",") != "b" {
		t.Fatalf("add=%v remove=%v", add, remove)
	}
	add, remove = DiffGroups([]string{"a"}, []string{"a"})
	if len(add) != 0 || len(remove) != 0 {
		t.Fatalf("unchanged add=%v remove=%v", add, remove)
	}
}

func TestFromEnv(t *testing.T) {
	base := map[string]string{
		"OTA_ADDON_TOKEN": "tok",
		"OTA_NAMESPACE":   "plant",
		"OTA_STATE_FILE":  "/tmp/ota.json",
		"API_URL":         "https://api.example:443",
	}
	getenv := func(k string) string { return base[k] }
	s, err := FromEnv(getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AllowURL("http://hawkbit:8080"); err == nil || !strings.Contains(err.Error(), "OTA_ALLOW_INSECURE") {
		t.Fatalf("http url error = %v", err)
	}
	if err := s.AllowURL("https://hawkbit"); err != nil {
		t.Fatal(err)
	}
	base["OTA_ALLOW_INSECURE"] = "1"
	s, err = FromEnv(getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AllowURL("http://hawkbit:8080"); err != nil {
		t.Fatal(err)
	}
	base["OTA_ALLOW_INSECURE"] = ""
	base["API_URL"] = "http://api:8000"
	if _, err := FromEnv(getenv); err == nil || !strings.Contains(err.Error(), "OTA_ALLOW_INSECURE") {
		t.Fatalf("http API_URL error = %v", err)
	}
	cases := []map[string]string{
		{"OTA_NAMESPACE": "plant", "OTA_STATE_FILE": "/tmp/ota.json", "API_URL": "http://api:8000"},
		{"OTA_ADDON_TOKEN": "tok", "OTA_STATE_FILE": "/tmp/ota.json", "API_URL": "http://api:8000"},
		{"OTA_ADDON_TOKEN": "tok", "OTA_NAMESPACE": "plant", "OTA_NAMESPACES": "a", "OTA_STATE_FILE": "/tmp/ota.json", "API_URL": "http://api:8000"},
		{"OTA_ADDON_TOKEN": "tok", "OTA_NAMESPACES": "a,,b", "OTA_STATE_FILE": "/tmp/ota.json", "API_URL": "http://api:8000"},
		{"OTA_ADDON_TOKEN": "tok", "OTA_NAMESPACE": "plant", "API_URL": "http://api:8000"},
		{"OTA_ADDON_TOKEN": "tok", "OTA_NAMESPACE": "plant", "OTA_STATE_FILE": "/tmp/ota.json"},
	}
	for _, env := range cases {
		_, err := FromEnv(func(k string) string { return env[k] })
		if err == nil {
			t.Fatalf("FromEnv(%v) accepted", env)
		}
	}
}

func TestPatchOmitsRequestedVersion(t *testing.T) {
	size := int64(4)
	d := desiredFields(Fields{Version: "1.2.0", URL: "https://example/a", SHA256: "ab", Size: &size, UpdatedAt: "2026-10-15T00:00:00Z"})
	r := reportedFields(Fields{Status: "downloading", Version: "1.2.0", Error: "canceled", ErrorSet: true, UpdatedAt: "2026-10-15T00:00:00Z"})
	for _, m := range []map[string]any{d, r} {
		if _, ok := m["requested_version"]; ok {
			t.Fatalf("patch has requested_version: %v", m)
		}
	}
	if _, err := shadowFrom(Patch{Device: "d", Desired: map[string]any{"requested_version": "1"}}); err == nil {
		t.Fatal("requested_version was accepted")
	}
	if _, err := shadowFrom(Patch{Device: "d", Reported: map[string]any{"progress": "1"}}); err == nil {
		t.Fatal("progress was accepted")
	}
}

func TestReportedPatchOnlyWhenChanged(t *testing.T) {
	dir := t.TempDir()
	b := &fakeBackend{snap: Snapshot{RawStatus: "downloading", ReportedVersion: "1.2.0"}}
	w := &fakeWriter{}
	r := testRunner(dir, b, w)
	msg := requested("dev", "1.2.0")
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.Open = script(msg)
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.count() != 1 {
		t.Fatalf("patches = %d, want 1", w.count())
	}
	if _, ok := w.patches[0].Reported["requested_version"]; ok {
		t.Fatal("reported patch has requested_version")
	}
	if w.patches[0].Reported["status"] != "downloading" {
		t.Fatalf("status = %v", w.patches[0].Reported["status"])
	}
}

func TestResyncDoesNotRepeatActions(t *testing.T) {
	dir := t.TempDir()
	b := &fakeBackend{snap: Snapshot{Version: "1.2.0", RawStatus: "downloading", ReportedVersion: "1.2.0"}}
	w := &fakeWriter{}
	r := testRunner(dir, b, w)
	added := &shadowpb.Shadow{Device: "dev", Event: shadowpb.ShadowEvent_DEVICE_ADDED, Tags: []string{"canary"}}
	r.Open = script(added, requested("dev", "1.2.0"))
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.Open = script(added, requested("dev", "1.2.0"), stateWith("dev", "1.2.0", "downloading"))
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.ensure != 1 || b.syncg != 1 || b.deploy != 1 || b.forget != 0 {
		t.Fatalf("ensure=%d sync=%d deploy=%d forget=%d", b.ensure, b.syncg, b.deploy, b.forget)
	}
	if w.count() != 1 {
		t.Fatalf("patches = %d, want 1", w.count())
	}
}

func TestQuietCodesAreNotRetried(t *testing.T) {
	for _, ce := range []*CallError{
		{Code: 9, Msg: "ota is not enabled"},
		{Code: 7, Msg: "ota provider does not match the namespace"},
	} {
		t.Run(ce.Msg, func(t *testing.T) {
			dir := t.TempDir()
			b := &fakeBackend{snap: Snapshot{RawStatus: "idle"}}
			w := &fakeWriter{err: ce}
			var buf bytes.Buffer
			log.SetOutput(&buf)
			t.Cleanup(func() { log.SetOutput(os.Stderr) })
			r := testRunner(dir, b, w)
			r.Open = script(requested("dev", "1.0.0"), requested("dev", "1.0.1"))
			if err := r.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if w.count() != 1 {
				t.Fatalf("patches = %d, want 1", w.count())
			}
			if b.deploy != 1 {
				t.Fatalf("deploy = %d, want 1", b.deploy)
			}
			if strings.Count(buf.String(), ce.Msg) != 1 {
				t.Fatalf("log = %q", buf.String())
			}
		})
	}
}

func TestRevokedTokenStops(t *testing.T) {
	dir := t.TempDir()
	b := &fakeBackend{snap: Snapshot{RawStatus: "idle"}}
	w := &fakeWriter{err: &CallError{Code: 16, Msg: "token is revoked"}}
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	r := testRunner(dir, b, w)
	r.Settings.Token = "super-secret"
	r.Open = script(requested("dev-1", "1.0.0"), requested("dev-2", "1.0.0"))
	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "inf plugins rotate") {
		t.Fatalf("err = %v", err)
	}
	if b.deploy != 1 {
		t.Fatalf("deploy = %d, want 1", b.deploy)
	}
	if !strings.Contains(buf.String(), "token is revoked; run inf plugins rotate and update the secret") {
		t.Fatalf("log = %q", buf.String())
	}
	if strings.Contains(buf.String(), "super-secret") {
		t.Fatal("log contains the addon token")
	}
}

func TestSkipDeployWritesFailedOnce(t *testing.T) {
	dir := t.TempDir()
	b := &fakeBackend{deployErr: &SkipDeploy{Msg: "unknown version 9"}, snap: Snapshot{RawStatus: "idle"}}
	w := &fakeWriter{}
	r := testRunner(dir, b, w)
	r.Open = script(requested("dev", "9"))
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.deploy != 1 || w.count() != 1 {
		t.Fatalf("deploy=%d patches=%d", b.deploy, w.count())
	}
	if w.patches[0].Reported["status"] != "failed" || w.patches[0].Reported["error"] != "unknown version 9" {
		t.Fatalf("reported = %v", w.patches[0].Reported)
	}
	r.Open = script(requested("dev", "9"))
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.deploy != 1 || w.count() != 1 {
		t.Fatalf("retry deploy=%d patches=%d", b.deploy, w.count())
	}
}

func TestHeldFailureFollowsANewAssignment(t *testing.T) {
	dir := t.TempDir()
	b := &fakeBackend{deployErr: &SkipDeploy{Msg: "unknown version 9"}, snap: Snapshot{RawStatus: "idle", Version: "1.0.0"}}
	w := &fakeWriter{}
	r := testRunner(dir, b, w)
	r.Open = script(requested("dev", "9"))
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.patches[0].Reported["status"] != "failed" {
		t.Fatalf("reported = %v", w.patches[0].Reported)
	}
	for _, p := range w.patches {
		if p.Reported["status"] == "idle" {
			t.Fatalf("idle replaced the failure: %#v", w.patches)
		}
	}
	n := w.count()
	r.Open = script(requested("dev", "9"))
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.count() != n {
		t.Fatalf("patches = %d, want %d", w.count(), n)
	}
	b.mu.Lock()
	b.snap = Snapshot{RawStatus: "success", Version: "2.0.0"}
	b.mu.Unlock()
	r.Poll = 20 * time.Millisecond
	r.Open = script()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	last := w.patches[w.count()-1]
	if last.Reported["status"] != "success" || last.Desired["version"] != "2.0.0" {
		t.Fatalf("last = %#v", last)
	}
}

func TestPollMirrorsEnsuredDevice(t *testing.T) {
	dir := t.TempDir()
	b := &fakeBackend{snap: Snapshot{Version: "9.9.0", RawStatus: "idle"}}
	w := &fakeWriter{}
	r := testRunner(dir, b, w)
	r.Open = script(&shadowpb.Shadow{Device: "dev", Event: shadowpb.ShadowEvent_DEVICE_ADDED})
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.count() != 0 || b.deploy != 0 {
		t.Fatalf("before poll patches=%d deploy=%d", w.count(), b.deploy)
	}
	r.Poll = 20 * time.Millisecond
	r.Open = script()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if w.count() != 1 || w.patches[0].Desired["version"] != "9.9.0" {
		t.Fatalf("patches = %d %v", w.count(), w.patches)
	}
	if b.deploy != 0 {
		t.Fatalf("poll deployed %d", b.deploy)
	}
}

func TestBackendErrorIsReportedOnce(t *testing.T) {
	dir := t.TempDir()
	b := &fakeBackend{snap: Snapshot{Error: "hawkbit unreachable"}}
	w := &fakeWriter{}
	r := testRunner(dir, b, w)
	r.Open = script(&shadowpb.Shadow{Device: "dev", Event: shadowpb.ShadowEvent_OTA_REQUESTED})
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.count() != 1 || w.patches[0].Reported["error"] != "hawkbit unreachable" {
		t.Fatalf("reported = %v", w.patches)
	}
	if w.patches[0].Reported["status"] != nil {
		t.Fatalf("status = %v", w.patches[0].Reported["status"])
	}
	r.Open = script(&shadowpb.Shadow{Device: "dev", Event: shadowpb.ShadowEvent_OTA_REQUESTED})
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.count() != 1 {
		t.Fatalf("patches = %d, want 1", w.count())
	}
}

func TestBackendDownStaysUp(t *testing.T) {
	dir := t.TempDir()
	b := &fakeBackend{ensureErr: errors.New("dial tcp: connection refused")}
	w := &fakeWriter{}
	r := testRunner(dir, b, w)
	r.Open = script(&shadowpb.Shadow{Device: "dev", Event: shadowpb.ShadowEvent_DEVICE_ADDED, Tags: []string{"canary"}})
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.count() != 1 || w.patches[0].Reported["error"] != "dial tcp: connection refused" {
		t.Fatalf("patches = %#v", w.patches)
	}
	if b.deploy != 0 || b.ensure != 1 {
		t.Fatalf("ensure=%d deploy=%d", b.ensure, b.deploy)
	}
	b.mu.Lock()
	b.ensureErr = nil
	b.mu.Unlock()
	r.Poll = 20 * time.Millisecond
	r.Open = script()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if b.ensure < 2 {
		t.Fatalf("poll did not retry ensure (%d)", b.ensure)
	}
}

func TestDeployDownIsRetried(t *testing.T) {
	dir := t.TempDir()
	b := &fakeBackend{deployErr: errors.New("dial tcp: connection refused")}
	w := &fakeWriter{}
	r := testRunner(dir, b, w)
	r.Open = script(requested("dev", "1.2.0"))
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.deploy != 1 || w.count() != 1 || w.patches[0].Reported["error"] != "dial tcp: connection refused" {
		t.Fatalf("deploy=%d patches=%#v", b.deploy, w.patches)
	}
	r.Open = script(requested("dev", "1.2.0"))
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.deploy != 2 || w.count() != 1 {
		t.Fatalf("deploy=%d patches=%d", b.deploy, w.count())
	}
}

func TestRequestWhileDownIsRetriedByPoll(t *testing.T) {
	dir := t.TempDir()
	b := &fakeBackend{ensureErr: errors.New("dial tcp: connection refused")}
	w := &fakeWriter{}
	r := testRunner(dir, b, w)
	r.Open = script(requested("dev", "1.2.0"))
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.deploy != 0 || b.ensure != 1 {
		t.Fatalf("ensure=%d deploy=%d", b.ensure, b.deploy)
	}
	b.mu.Lock()
	b.ensureErr = nil
	b.mu.Unlock()
	r.Poll = 20 * time.Millisecond
	r.Open = script()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if b.ensure < 2 || b.deploy < 1 {
		t.Fatalf("ensure=%d deploy=%d", b.ensure, b.deploy)
	}
}

func TestRevokedStreamStopsTheProcess(t *testing.T) {
	for _, poll := range []time.Duration{0, 20 * time.Millisecond} {
		t.Run(poll.String(), func(t *testing.T) {
			dir := t.TempDir()
			w := &fakeWriter{}
			var buf bytes.Buffer
			log.SetOutput(&buf)
			t.Cleanup(func() { log.SetOutput(os.Stderr) })
			r := testRunner(dir, &fakeBackend{}, w)
			r.Poll = poll
			r.Settings.Token = "super-secret"
			r.Open = func(context.Context, string, uint64) (<-chan *shadowpb.Shadow, <-chan error) {
				ch := make(chan *shadowpb.Shadow)
				close(ch)
				errc := make(chan error, 1)
				errc <- errRevoked
				close(errc)
				return ch, errc
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := r.Run(ctx)
			if !errors.Is(err, errRevoked) {
				t.Fatalf("err = %v", err)
			}
			if !strings.Contains(buf.String(), errRevoked.Error()) || strings.Contains(buf.String(), "super-secret") {
				t.Fatalf("log = %q", buf.String())
			}
		})
	}
}

func TestPatchRetriesBackOff(t *testing.T) {
	dir := t.TempDir()
	st, err := loadStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	w := &fakeWriter{err: errors.New("unavailable"), remain: 2, stop: true}
	var waits []time.Duration
	r := testRunner(dir, &fakeBackend{}, w)
	r.Sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	err = r.write(context.Background(), st, "plant", Patch{Device: "dev", Reported: map[string]any{"status": "idle"}})
	if err != nil {
		t.Fatal(err)
	}
	if w.count() != 3 || len(waits) != 2 {
		t.Fatalf("patches=%d waits=%v", w.count(), waits)
	}
	if waits[0] < 200*time.Millisecond || waits[0] >= 250*time.Millisecond {
		t.Fatalf("first wait %s", waits[0])
	}
	if waits[1] < 400*time.Millisecond || waits[1] >= 500*time.Millisecond {
		t.Fatalf("second wait %s", waits[1])
	}
}

func TestSleepStopsWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := (Runner{}).sleep(ctx, time.Minute)
	if !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Fatalf("err=%v after %s", err, time.Since(start))
	}
}

func TestRequestAlreadySetAtSubscribe(t *testing.T) {
	dir := t.TempDir()
	b := &fakeBackend{}
	w := &fakeWriter{}
	r := testRunner(dir, b, w)
	msg := requested("dev", "1.2.0")
	msg.Event = 0
	msg.Sequence = 8
	r.Open = script(msg)
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.deploy != 1 {
		t.Fatalf("deploy = %d", b.deploy)
	}
	var resume uint64
	r.Open = func(_ context.Context, _ string, got uint64) (<-chan *shadowpb.Shadow, <-chan error) {
		resume = got
		return script()(context.Background(), "", got)
	}
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if resume != 8 || b.deploy != 1 {
		t.Fatalf("resume=%d deploy=%d", resume, b.deploy)
	}
}

func TestAddedWithRequestDeploys(t *testing.T) {
	dir := t.TempDir()
	b := &fakeBackend{}
	w := &fakeWriter{}
	r := testRunner(dir, b, w)
	msg := requested("dev", "1.2.0")
	msg.Event = shadowpb.ShadowEvent_DEVICE_ADDED
	r.Open = script(msg)
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.deploy != 1 || b.ensure != 1 {
		t.Fatalf("ensure=%d deploy=%d", b.ensure, b.deploy)
	}
}

func TestUnknownStatusIsLoggedOnce(t *testing.T) {
	dir := t.TempDir()
	b := &fakeBackend{snap: Snapshot{RawStatus: "nope"}}
	w := &fakeWriter{}
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	r := testRunner(dir, b, w)
	r.Open = script(
		&shadowpb.Shadow{Device: "dev", Event: shadowpb.ShadowEvent_STATE},
		&shadowpb.Shadow{Device: "dev", Event: shadowpb.ShadowEvent_STATE},
	)
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Count(buf.String(), `ota status "nope"`) != 1 {
		t.Fatalf("log = %q", buf.String())
	}
}

func TestSuccessfulStatusClearsTheError(t *testing.T) {
	dir := t.TempDir()
	b := &fakeBackend{snap: Snapshot{Error: "hawkbit unreachable"}}
	w := &fakeWriter{}
	r := testRunner(dir, b, w)
	r.Open = script(&shadowpb.Shadow{Device: "dev", Event: shadowpb.ShadowEvent_STATE})
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.patches[len(w.patches)-1].Reported["error"] != "hawkbit unreachable" {
		t.Fatalf("patches = %#v", w.patches)
	}
	b.snap = Snapshot{RawStatus: "success"}
	r.Open = script(&shadowpb.Shadow{Device: "dev", Event: shadowpb.ShadowEvent_STATE})
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	last := w.patches[len(w.patches)-1]
	if last.Reported["error"] != "" || last.Reported["status"] != "success" {
		t.Fatalf("last = %#v", last.Reported)
	}
}

func TestWriterSendsAddonToken(t *testing.T) {
	c := &fakeShadow{}
	w := NewWriter(c, "addon-tok")
	err := w.Patch(context.Background(), Patch{
		Device:   "dev",
		Desired:  map[string]any{"version": "1.2.0"},
		Reported: map[string]any{"status": "idle"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.auth) != 1 || c.auth[0] != "Bearer addon-tok" {
		t.Fatalf("auth = %v", c.auth)
	}
	if c.got.GetDevice() != "dev" {
		t.Fatalf("device = %s", c.got.GetDevice())
	}
	if _, ok := c.got.GetDesired().GetData().GetFields()["ota"].GetStructValue().GetFields()["requested_version"]; ok {
		t.Fatal("desired patch has requested_version")
	}
	if _, ok := c.got.GetReported().GetData().GetFields()["ota"].GetStructValue().GetFields()["url"]; ok {
		t.Fatal("reported patch has a non-ota desired key")
	}
}

type fakeBackend struct {
	mu        sync.Mutex
	snap      Snapshot
	deployErr error
	ensureErr error
	ensure    int
	syncg     int
	deploy    int
	forget    int
}

func (f *fakeBackend) Name() string { return "fake" }

func (f *fakeBackend) EnsureDevice(context.Context, Device) error {
	f.mu.Lock()
	f.ensure++
	err := f.ensureErr
	f.mu.Unlock()
	return err
}

func (f *fakeBackend) SyncGroup(context.Context, Device, []string, []string) error {
	f.mu.Lock()
	f.syncg++
	f.mu.Unlock()
	return nil
}

func (f *fakeBackend) Deploy(context.Context, Device, string) error {
	f.mu.Lock()
	f.deploy++
	err := f.deployErr
	f.mu.Unlock()
	return err
}

func (f *fakeBackend) Status(context.Context, Device) (Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap, nil
}

func (f *fakeBackend) Forget(context.Context, Device) error {
	f.mu.Lock()
	f.forget++
	f.mu.Unlock()
	return nil
}

type fakeWriter struct {
	mu      sync.Mutex
	patches []Patch
	err     error
	remain  int
	stop    bool
}

func (f *fakeWriter) Patch(_ context.Context, p Patch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patches = append(f.patches, p)
	if f.remain > 0 {
		f.remain--
		return f.err
	}
	if f.stop {
		return nil
	}
	return f.err
}

func (f *fakeWriter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.patches)
}

func testRunner(dir string, b *fakeBackend, w *fakeWriter) Runner {
	return Runner{
		Settings: Settings{
			Token:      "tok",
			Namespaces: []string{"plant"},
			StateFile:  filepath.Join(dir, "state.json"),
			APIURL:     "http://api:8000",
		},
		Backend: b,
		Writer:  w,
		Open:    script(requested("dev", "1.2.0")),
		Status: map[string]Mapped{
			"idle":        {Status: "idle"},
			"downloading": {Status: "downloading"},
			"installing":  {Status: "installing"},
			"success":     {Status: "success"},
			"failed":      {Status: "failed"},
		},
		Now: func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) },
	}
}

func script(msgs ...*shadowpb.Shadow) StreamOpen {
	return func(ctx context.Context, _ string, _ uint64) (<-chan *shadowpb.Shadow, <-chan error) {
		ch := make(chan *shadowpb.Shadow)
		errc := make(chan error)
		go func() {
			defer close(errc)
			defer close(ch)
			for _, m := range msgs {
				select {
				case <-ctx.Done():
					return
				case ch <- m:
				}
			}
		}()
		return ch, errc
	}
}

func requested(device, version string) *shadowpb.Shadow {
	return &shadowpb.Shadow{
		Device:  device,
		Event:   shadowpb.ShadowEvent_OTA_REQUESTED,
		Desired: shadowOTA(map[string]any{"requested_version": version}),
	}
}

func stateWith(device, version, status string) *shadowpb.Shadow {
	return &shadowpb.Shadow{
		Device:   device,
		Event:    shadowpb.ShadowEvent_STATE,
		Desired:  shadowOTA(map[string]any{"version": version}),
		Reported: shadowOTA(map[string]any{"status": status, "version": version}),
	}
}

func shadowOTA(fields map[string]any) *shadowpb.State {
	ota, err := structpb.NewStruct(fields)
	if err != nil {
		panic(err)
	}
	return &shadowpb.State{Data: &structpb.Struct{Fields: map[string]*structpb.Value{
		"ota": structpb.NewStructValue(ota),
	}}}
}

type fakeShadow struct {
	pb.ShadowServiceClient
	got  *shadowpb.Shadow
	auth []string
}

func (f *fakeShadow) Patch(ctx context.Context, in *shadowpb.Shadow, _ ...grpc.CallOption) (*shadowpb.Shadow, error) {
	md, _ := metadata.FromOutgoingContext(ctx)
	f.auth = append(f.auth, md.Get("authorization")...)
	f.got = in
	return in, nil
}
