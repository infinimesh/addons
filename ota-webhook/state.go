package main

import (
	"encoding/json"
	"log"
	"os"
	"sort"
	"sync"

	shadowpb "github.com/infinimesh/proto/shadow"
)

const sentCap = 1024

type pending struct {
	ID   uint64 `json:"id"`
	URL  string `json:"url"`
	Body []byte `json:"body"`
}

type seenDevice struct {
	Title string   `json:"title,omitempty"`
	Tags  []string `json:"tags,omitempty"`
}

type disk struct {
	Sent    []uint64              `json:"sent,omitempty"`
	Pending []pending             `json:"pending,omitempty"`
	Seq     map[string]uint64     `json:"seq,omitempty"`
	Devices map[string]seenDevice `json:"devices,omitempty"`
}

type book struct {
	mu       sync.Mutex
	path     string
	sent     map[uint64]struct{}
	order    []uint64
	pending  map[uint64]pending
	inflight map[uint64]struct{}
	seq      map[string]uint64
	quiet    map[string]struct{}
	devices  map[string]seenDevice
}

func loadBook(path string) (*book, error) {
	b := &book{
		path:     path,
		sent:     map[uint64]struct{}{},
		pending:  map[uint64]pending{},
		inflight: map[uint64]struct{}{},
		seq:      map[string]uint64{},
		quiet:    map[string]struct{}{},
		devices:  map[string]seenDevice{},
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return b, nil
		}
		return nil, err
	}
	var d disk
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	for _, id := range d.Sent {
		if _, ok := b.sent[id]; ok {
			continue
		}
		b.sent[id] = struct{}{}
		b.order = append(b.order, id)
	}
	for _, p := range d.Pending {
		if p.ID == 0 {
			continue
		}
		b.pending[p.ID] = p
	}
	if d.Seq != nil {
		b.seq = d.Seq
	}
	if d.Devices != nil {
		b.devices = d.Devices
	}
	return b, nil
}

func deviceKey(ns, id string) string { return ns + "\n" + id }

func normTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	out := append([]string(nil), tags...)
	sort.Strings(out)
	return out
}

func sameTags(a, b []string) bool {
	a, b = normTags(a), normTags(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (b *book) sameDevice(ns string, msg *shadowpb.Shadow) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	name := msg.GetNamespace()
	if name == "" {
		name = ns
	}
	got, ok := b.devices[deviceKey(name, msg.GetDevice())]
	return ok && got.Title == msg.GetTitle() && sameTags(got.Tags, msg.GetTags())
}

func (b *book) noteDevice(ns string, msg *shadowpb.Shadow) {
	if msg == nil || msg.GetDevice() == "" {
		return
	}
	name := msg.GetNamespace()
	if name == "" {
		name = ns
	}
	key := deviceKey(name, msg.GetDevice())
	b.mu.Lock()
	defer b.mu.Unlock()
	switch msg.GetEvent() {
	case shadowpb.ShadowEvent_DEVICE_REMOVED:
		delete(b.devices, key)
	case shadowpb.ShadowEvent_DEVICE_ADDED, shadowpb.ShadowEvent_TAG_ADDED, shadowpb.ShadowEvent_TAG_REMOVED:
		prev := b.devices[key]
		title := msg.GetTitle()
		if title == "" {
			title = prev.Title
		}
		b.devices[key] = seenDevice{Title: title, Tags: normTags(msg.GetTags())}
	default:
		return
	}
	_ = b.saveLocked()
}

func (b *book) seqOf(ns string) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seq[ns]
}

func (b *book) advance(ns string, seq uint64) {
	if seq == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seq[ns] >= seq {
		return
	}
	b.seq[ns] = seq
	_ = b.saveLocked()
}

func (b *book) sentAlready(id uint64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.sent[id]
	return ok
}

func (b *book) start(id uint64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.sent[id]; ok {
		return false
	}
	if _, ok := b.inflight[id]; ok {
		return false
	}
	b.inflight[id] = struct{}{}
	return true
}

func (b *book) finish(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.inflight, id)
}

func (b *book) savePending(id uint64, url string, body []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending[id] = pending{ID: id, URL: url, Body: append([]byte(nil), body...)}
	_ = b.saveLocked()
}

func (b *book) drop(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.pending[id]; !ok {
		return
	}
	delete(b.pending, id)
	_ = b.saveLocked()
}

func (b *book) markSent(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, id)
	if _, ok := b.sent[id]; !ok {
		b.sent[id] = struct{}{}
		b.order = append(b.order, id)
		for len(b.order) > sentCap {
			old := b.order[0]
			b.order = b.order[1:]
			delete(b.sent, old)
		}
	}
	_ = b.saveLocked()
}

func (b *book) pendingList() []pending {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]pending, 0, len(b.pending))
	for _, p := range b.pending {
		p.Body = append([]byte(nil), p.Body...)
		out = append(out, p)
	}
	return out
}

func (b *book) once(ns, msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.quiet[ns]; ok {
		return
	}
	b.quiet[ns] = struct{}{}
	log.Printf("%s\n", msg)
}

func (b *book) saveLocked() error {
	d := disk{Sent: append([]uint64(nil), b.order...), Seq: b.seq, Devices: b.devices}
	for _, p := range b.pending {
		d.Pending = append(d.Pending, p)
	}
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.path)
}
