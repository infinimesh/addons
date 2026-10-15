package main

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"strings"

	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/protobuf/types/known/structpb"
)

func forwardEvent(ev shadowpb.ShadowEvent) bool {
	switch ev {
	case shadowpb.ShadowEvent_DEVICE_ADDED,
		shadowpb.ShadowEvent_DEVICE_REMOVED,
		shadowpb.ShadowEvent_TAG_ADDED,
		shadowpb.ShadowEvent_TAG_REMOVED,
		shadowpb.ShadowEvent_OTA_TARGET_SET,
		shadowpb.ShadowEvent_OTA_STATUS_CHANGED,
		shadowpb.ShadowEvent_OTA_REQUESTED:
		return true
	default:
		return false
	}
}

type outbound struct {
	EventID   uint64         `json:"event_id"`
	Event     string         `json:"event"`
	Sequence  uint64         `json:"sequence,omitempty"`
	Device    string         `json:"device"`
	Namespace string         `json:"namespace,omitempty"`
	Title     string         `json:"title,omitempty"`
	Tags      []string       `json:"tags,omitempty"`
	Tag       string         `json:"tag,omitempty"`
	Desired   map[string]any `json:"desired,omitempty"`
	Reported  map[string]any `json:"reported,omitempty"`
	Snapshot  bool           `json:"snapshot,omitempty"`
}

func eventDoc(ns string, msg *shadowpb.Shadow) outbound {
	name := msg.GetNamespace()
	if name == "" {
		name = ns
	}
	doc := outbound{
		EventID:   msg.GetEventId(),
		Event:     msg.GetEvent().String(),
		Sequence:  msg.GetSequence(),
		Device:    msg.GetDevice(),
		Namespace: name,
		Title:     msg.GetTitle(),
		Tags:      append([]string(nil), msg.GetTags()...),
		Tag:       msg.GetTag(),
		Desired:   otaObject(msg.GetDesired(), []string{"version", "requested_version", "url", "sha256", "size", "updated_at"}),
		Reported:  otaObject(msg.GetReported(), []string{"status", "version", "error", "updated_at"}),
	}
	return doc
}

func otaObject(st *shadowpb.State, keys []string) map[string]any {
	if st == nil || st.GetData() == nil {
		return nil
	}
	v, ok := st.GetData().GetFields()["ota"]
	if !ok || v.GetStructValue() == nil {
		return nil
	}
	fields := v.GetStructValue().GetFields()
	out := map[string]any{}
	for _, key := range keys {
		val, ok := fields[key]
		if !ok {
			continue
		}
		got, ok := fieldValue(val)
		if ok {
			out[key] = got
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func fieldValue(v *structpb.Value) (any, bool) {
	switch k := v.Kind.(type) {
	case *structpb.Value_StringValue:
		if k.StringValue == "" {
			return nil, false
		}
		return k.StringValue, true
	case *structpb.Value_NumberValue:
		if k.NumberValue == math.Trunc(k.NumberValue) {
			return int64(k.NumberValue), true
		}
		return k.NumberValue, true
	default:
		return nil, false
	}
}

func (a *app) handle(ctx context.Context, ns string, msg *shadowpb.Shadow) error {
	if msg == nil || msg.GetDevice() == "" || !forwardEvent(msg.GetEvent()) {
		return nil
	}
	if msg.GetEvent() == shadowpb.ShadowEvent_DEVICE_ADDED && msg.GetSequence() == 0 && a.book.sameDevice(ns, msg) {
		return nil
	}
	return a.deliver(ctx, ns, msg)
}

func (a *app) deliver(ctx context.Context, ns string, msg *shadowpb.Shadow) error {
	id := msg.GetEventId()
	if id != 0 && a.book.sentAlready(id) {
		return nil
	}
	if id != 0 && !a.book.start(id) {
		return nil
	}
	if id != 0 {
		defer a.book.finish(id)
	}
	doc := eventDoc(ns, msg)
	if msg.GetEvent() == shadowpb.ShadowEvent_DEVICE_ADDED && msg.GetSequence() == 0 {
		doc.Snapshot = true
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	url := a.urlOf(ns)
	if msg.GetNamespace() != "" {
		if u := a.urlOf(msg.GetNamespace()); u != "" {
			url = u
		}
	}
	if url == "" {
		a.book.once(ns, "webhook_url is required when provider is webhook")
		return nil
	}
	if id != 0 {
		a.book.savePending(id, url, body)
	}
	if err := a.post.send(ctx, url, body); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if id != 0 && !retryLater(err) {
			a.book.drop(id)
		}
		log.Printf("namespace %s event %d: %s\n", ns, id, err)
		return nil
	}
	if id != 0 {
		a.book.markSent(id)
	}
	a.book.noteDevice(ns, msg)
	return nil
}

func retryLater(err error) bool {
	return err != nil && strings.Contains(err.Error(), "gave up after")
}

func (a *app) flush(ctx context.Context) {
	for _, p := range a.book.pendingList() {
		if ctx.Err() != nil {
			return
		}
		if !a.book.start(p.ID) {
			continue
		}
		err := a.post.send(ctx, p.URL, p.Body)
		a.book.finish(p.ID)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !retryLater(err) {
				a.book.drop(p.ID)
			}
			log.Printf("event %d: %s\n", p.ID, err)
			continue
		}
		a.book.markSent(p.ID)
	}
}
