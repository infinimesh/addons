package otacore

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// StreamOpen is one namespace stream. StreamNamespaceToken is the production opener.
type StreamOpen func(ctx context.Context, namespace string) <-chan *shadowpb.Shadow

// Runner watches the namespaces in Settings and calls Backend.
type Runner struct {
	Settings Settings
	Backend  Backend
	Open     StreamOpen
	Writer   Writer
	Status   map[string]Mapped
	Now      func() time.Time
	// Poll asks the backend for each known device on this interval.
	// Zero disables it. hawkBit uses it to mirror a deployment started there.
	Poll time.Duration
}

// SkipDeploy is a version the backend will not deploy. It is written once
// as reported status failed and is not retried.
type SkipDeploy struct{ Msg string }

func (e *SkipDeploy) Error() string {
	if e == nil || e.Msg == "" {
		return "deploy skipped"
	}
	return e.Msg
}

var errRevoked = errors.New("token is revoked; run inf plugins rotate and update the secret")

// Run reads the state file, streams each namespace, and returns when ctx is
// done or the addon token is revoked.
func (r Runner) Run(ctx context.Context) error {
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.Open == nil || r.Writer == nil || r.Backend == nil {
		return errors.New("backend, stream, and writer are required")
	}
	st, err := loadStore(r.Settings.StateFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	n := len(r.Settings.Namespaces)
	if r.Poll > 0 {
		n++
	}
	errc := make(chan error, n)
	for _, ns := range r.Settings.Namespaces {
		wg.Add(1)
		go func(ns string) {
			defer wg.Done()
			errc <- r.runNS(ctx, st, ns)
		}(ns)
	}
	if r.Poll > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.poll(ctx, st); err != nil {
				errc <- err
			}
		}()
	}
	go func() {
		wg.Wait()
		close(errc)
	}()
	var first error
	for err := range errc {
		if err == nil {
			continue
		}
		if first == nil {
			first = err
			cancel()
		}
	}
	if errors.Is(first, errRevoked) {
		log.Printf("%s\n", errRevoked.Error())
	}
	return first
}

func (r Runner) runNS(ctx context.Context, st *store, ns string) error {
	for msg := range r.Open(ctx, ns) {
		if err := r.handle(ctx, st, ns, msg); err != nil {
			return err
		}
	}
	return nil
}

func (r Runner) poll(ctx context.Context, st *store) error {
	t := time.NewTicker(r.Poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			for _, d := range st.list() {
				if err := r.mirror(ctx, st, d, nil); err != nil {
					if errors.Is(err, errRevoked) || ctx.Err() != nil {
						return err
					}
					log.Printf("namespace %s: %s\n", d.Namespace, err)
				}
			}
		}
	}
}

func (r Runner) handle(ctx context.Context, st *store, ns string, msg *shadowpb.Shadow) error {
	if st.quiet(ns) || msg == nil {
		return nil
	}
	id := msg.GetDevice()
	if id == "" {
		return nil
	}
	d := Device{ID: id, Namespace: ns, Title: msg.GetTitle(), Tags: append([]string(nil), msg.GetTags()...)}
	switch msg.GetEvent() {
	case shadowpb.ShadowEvent_DEVICE_REMOVED:
		return r.forget(ctx, st, d)
	case shadowpb.ShadowEvent_DEVICE_ADDED, shadowpb.ShadowEvent_TAG_ADDED, shadowpb.ShadowEvent_TAG_REMOVED:
		return r.ensure(ctx, st, d, true)
	case shadowpb.ShadowEvent_OTA_REQUESTED:
		if err := r.ensure(ctx, st, d, false); err != nil {
			return err
		}
		if err := r.deploy(ctx, st, d, otaString(msg.GetDesired(), "requested_version")); err != nil {
			return err
		}
		return r.mirror(ctx, st, d, msg)
	case shadowpb.ShadowEvent_OTA_STATUS_CHANGED:
		return r.noteReported(st, d, msg)
	case shadowpb.ShadowEvent_STATE:
		if err := r.ensure(ctx, st, d, false); err != nil {
			return err
		}
		if err := r.deploy(ctx, st, d, otaString(msg.GetDesired(), "requested_version")); err != nil {
			return err
		}
		return r.mirror(ctx, st, d, msg)
	default:
		return nil
	}
}

func devKey(ns, id string) string { return ns + "/" + id }

func (r Runner) ensure(ctx context.Context, st *store, d Device, tagsKnown bool) error {
	if st.quiet(d.Namespace) {
		return nil
	}
	key := devKey(d.Namespace, d.ID)
	rec, _ := st.get(key)
	rec.Namespace = d.Namespace
	if !rec.Ensured {
		if err := r.Backend.EnsureDevice(ctx, d); err != nil {
			return err
		}
		rec.Ensured = true
	}
	if tagsKnown {
		add, remove := DiffGroups(rec.Tags, d.Tags)
		if len(add) > 0 || len(remove) > 0 {
			if err := r.Backend.SyncGroup(ctx, d, add, remove); err != nil {
				return err
			}
			rec.Tags = append([]string(nil), d.Tags...)
		}
	}
	st.put(key, rec)
	return nil
}

func (r Runner) forget(ctx context.Context, st *store, d Device) error {
	key := devKey(d.Namespace, d.ID)
	rec, ok := st.get(key)
	if !ok || !rec.Ensured {
		return nil
	}
	if err := r.Backend.Forget(ctx, d); err != nil {
		return err
	}
	st.del(key)
	return nil
}

func (r Runner) deploy(ctx context.Context, st *store, d Device, version string) error {
	if st.quiet(d.Namespace) || version == "" {
		return nil
	}
	key := devKey(d.Namespace, d.ID)
	rec, _ := st.get(key)
	idKey := IdempotencyKey(d.ID, version)
	if rec.Deployed == idKey {
		return nil
	}
	if err := r.Backend.Deploy(ctx, d, version); err != nil {
		var skip *SkipDeploy
		if errors.As(err, &skip) {
			rec.Namespace = d.Namespace
			rec.Deployed = idKey
			rec.Held = true
			rec.HeldVersion = rec.Desired
			st.put(key, rec)
			return r.fail(ctx, st, d, skip.Error())
		}
		return err
	}
	rec.Namespace = d.Namespace
	rec.Deployed = idKey
	rec.Held = false
	rec.SawHold = false
	rec.HeldVersion = ""
	st.put(key, rec)
	return nil
}

func (r Runner) fail(ctx context.Context, st *store, d Device, msg string) error {
	key := devKey(d.Namespace, d.ID)
	rec, _ := st.get(key)
	now := r.Now().UTC().Format(time.RFC3339)
	p := Patch{Device: d.ID, Reported: reportedFields(Fields{
		Status: "failed", Error: msg, ErrorSet: true, UpdatedAt: now,
	})}
	if err := r.write(ctx, st, d.Namespace, p); err != nil {
		return err
	}
	rec.Namespace = d.Namespace
	rec.Status = "failed"
	rec.Error = msg
	rec.ErrorSet = true
	st.put(key, rec)
	return nil
}

func (r Runner) noteReported(st *store, d Device, msg *shadowpb.Shadow) error {
	key := devKey(d.Namespace, d.ID)
	rec, _ := st.get(key)
	rec.Namespace = d.Namespace
	if s := otaString(msg.GetReported(), "status"); s != "" {
		rec.Status = s
	}
	if v := otaString(msg.GetReported(), "version"); v != "" {
		rec.Version = v
	}
	if _, ok := otaValue(msg.GetReported(), "error"); ok {
		rec.Error = otaString(msg.GetReported(), "error")
		rec.ErrorSet = true
	}
	st.put(key, rec)
	return nil
}

func (r Runner) mirror(ctx context.Context, st *store, d Device, msg *shadowpb.Shadow) error {
	if st.quiet(d.Namespace) {
		return nil
	}
	snap, err := r.Backend.Status(ctx, d)
	if err != nil {
		return err
	}
	key := devKey(d.Namespace, d.ID)
	rec, _ := st.get(key)
	rec.Namespace = d.Namespace
	now := r.Now().UTC().Format(time.RFC3339)

	desired := Fields{UpdatedAt: now}
	if snap.Version != "" && snap.Version != shadowOr(otaString(msg.GetDesired(), "version"), rec.Desired) {
		desired.Version = snap.Version
	}
	if snap.URL != "" && snap.URL != shadowOr(otaString(msg.GetDesired(), "url"), rec.URL) {
		desired.URL = snap.URL
	}
	if snap.SHA256 != "" && snap.SHA256 != shadowOr(otaString(msg.GetDesired(), "sha256"), rec.SHA256) {
		desired.SHA256 = snap.SHA256
	}
	if snap.Size != nil {
		cur, ok := otaSize(msg.GetDesired())
		if !ok && rec.SizeSet && rec.Size != nil {
			cur, ok = *rec.Size, true
		}
		if !ok || cur != *snap.Size {
			n := *snap.Size
			desired.Size = &n
		}
	}

	if rec.Held {
		if !rec.SawHold {
			rec.SawHold = true
			if snap.Version != "" {
				rec.HeldVersion = snap.Version
			}
		} else if snap.Version != "" && snap.Version != rec.HeldVersion {
			rec.Held = false
			rec.SawHold = false
			rec.HeldVersion = ""
		}
	}
	reported := Fields{UpdatedAt: now}
	if rec.Held {
		snap.RawStatus = ""
		snap.Error = ""
	}
	if snap.RawStatus != "" {
		mapped, ok := MapStatus(r.Status, snap.RawStatus)
		if !ok {
			log.Printf("namespace %s: ota status %q is not in the status table\n", d.Namespace, snap.RawStatus)
		} else {
			curStatus := shadowOr(otaString(msg.GetReported(), "status"), rec.Status)
			curVersion := shadowOr(otaString(msg.GetReported(), "version"), rec.Version)
			curErr := rec.Error
			if v, ok := otaValue(msg.GetReported(), "error"); ok {
				curErr = v
			}
			if mapped.Status != curStatus {
				reported.Status = mapped.Status
			}
			if snap.ReportedVersion != "" && snap.ReportedVersion != curVersion {
				reported.Version = snap.ReportedVersion
			}
			if mapped.Error != curErr {
				reported.Error = mapped.Error
				reported.ErrorSet = true
			}
		}
	}
	if snap.Error != "" && !reported.ErrorSet {
		curErr := rec.Error
		if v, ok := otaValue(msg.GetReported(), "error"); ok {
			curErr = v
		}
		if snap.Error != curErr {
			reported.Error = snap.Error
			reported.ErrorSet = true
		}
	}

	p := Patch{Device: d.ID, Desired: desiredFields(desired), Reported: reportedFields(reported)}
	if err := r.write(ctx, st, d.Namespace, p); err != nil {
		return err
	}
	if p.Desired["version"] != nil {
		rec.Desired = snap.Version
	}
	if p.Desired["url"] != nil {
		rec.URL = snap.URL
	}
	if p.Desired["sha256"] != nil {
		rec.SHA256 = snap.SHA256
	}
	if p.Desired["size"] != nil && snap.Size != nil {
		n := *snap.Size
		rec.Size = &n
		rec.SizeSet = true
	}
	if p.Reported["status"] != nil {
		rec.Status, _ = p.Reported["status"].(string)
	}
	if p.Reported["version"] != nil {
		rec.Version, _ = p.Reported["version"].(string)
	}
	if _, ok := p.Reported["error"]; ok {
		rec.Error, _ = p.Reported["error"].(string)
		rec.ErrorSet = true
	}
	st.put(key, rec)
	return nil
}

func shadowOr(fromMsg, stored string) string {
	if fromMsg != "" {
		return fromMsg
	}
	return stored
}

func (r Runner) write(ctx context.Context, st *store, ns string, p Patch) error {
	if len(p.Desired) == 0 && len(p.Reported) == 0 {
		return nil
	}
	var err error
	for range 3 {
		err = r.Writer.Patch(ctx, p)
		if err == nil {
			return nil
		}
		if ce := asCall(err); ce != nil {
			if ce.Code == 16 || strings.Contains(ce.Msg, "token is revoked") {
				return errRevoked
			}
			if ce.Code == 7 || ce.Code == 9 {
				if st.markQuiet(ns, ce.Msg) {
					log.Printf("namespace %s: %s\n", ns, ce.Msg)
				}
				return nil
			}
			if ce.Code == 3 {
				log.Printf("namespace %s: %s\n", ns, ce.Msg)
				return nil
			}
		}
	}
	log.Printf("namespace %s: %s\n", ns, err)
	return err
}

func asCall(err error) *CallError {
	var ce *CallError
	if errors.As(err, &ce) {
		return ce
	}
	if st, ok := status.FromError(err); ok {
		return &CallError{Code: int(st.Code()), Msg: st.Message()}
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "token is revoked"):
		return &CallError{Code: 16, Msg: "token is revoked"}
	case strings.Contains(msg, "ota is not enabled"):
		return &CallError{Code: 9, Msg: "ota is not enabled"}
	case strings.Contains(msg, "ota provider does not match the namespace"):
		return &CallError{Code: 7, Msg: "ota provider does not match the namespace"}
	default:
		return nil
	}
}

// CallError is a core status code and its message.
type CallError struct {
	Code int
	Msg  string
}

func (e *CallError) Error() string { return e.Msg }

func otaObject(st *shadowpb.State) map[string]*structpb.Value {
	if st == nil || st.GetData() == nil {
		return nil
	}
	v := st.GetData().GetFields()["ota"]
	if v == nil || v.GetStructValue() == nil {
		return nil
	}
	return v.GetStructValue().GetFields()
}

func otaString(st *shadowpb.State, key string) string {
	v, ok := otaValue(st, key)
	if !ok {
		return ""
	}
	return v
}

func otaValue(st *shadowpb.State, key string) (string, bool) {
	fields := otaObject(st)
	if fields == nil {
		return "", false
	}
	v, ok := fields[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.Kind.(*structpb.Value_StringValue)
	if !ok {
		return "", false
	}
	return s.StringValue, true
}

func otaSize(st *shadowpb.State) (int64, bool) {
	fields := otaObject(st)
	if fields == nil {
		return 0, false
	}
	v, ok := fields["size"]
	if !ok || v == nil {
		return 0, false
	}
	n, ok := v.Kind.(*structpb.Value_NumberValue)
	if !ok {
		return 0, false
	}
	return int64(n.NumberValue), true
}
