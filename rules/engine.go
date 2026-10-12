package main

import (
	"log"
	"sync"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/message"
	shadowpb "github.com/infinimesh/proto/shadow"
)

// Engine keeps the last known state of each device and checks the rules on
// every update.
type Engine struct {
	rules   []*Rule
	store   *Store
	actions *Actions
	now     func() time.Time

	mu   sync.Mutex
	last map[string]*message.Message
}

// merge keeps the parts an update does not carry from the last update, so a
// connection change still sees the last reported data.
func (e *Engine) merge(m *message.Message) *message.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.last == nil {
		e.last = map[string]*message.Message{}
	}
	prev := e.last[m.Device]
	if prev != nil {
		if m.Reported == nil {
			m.Reported = prev.Reported
		}
		if m.Desired == nil {
			m.Desired = prev.Desired
		}
		if m.Connection == nil {
			m.Connection = prev.Connection
		}
	}
	e.last[m.Device] = m
	return m
}

func (e *Engine) Handle(obj api.Object, s *shadowpb.Shadow) {
	m, ok := message.New(obj, s, false, e.now())
	if !ok {
		return
	}
	e.Check(obj, e.merge(m))
}

// Check runs every rule on one merged device state.
func (e *Engine) Check(obj api.Object, m *message.Message) {
	v := Vars{Device: obj.UID, Title: obj.Name, Tags: obj.Tags, Namespace: obj.NamespaceName}
	if m.Reported != nil {
		v.Reported = m.Reported.Data
	}
	if m.Desired != nil {
		v.Desired = m.Desired.Data
	}
	if m.Connection != nil {
		v.Connected = m.Connection.Connected
	}
	now := e.now()
	for _, r := range e.rules {
		when, whenOK := eval(r.when, v)
		clear, clearOK := !when, whenOK
		if r.clear != nil {
			clear, clearOK = eval(r.clear, v)
		}
		ev, err := e.store.Step(r, obj.UID, obj.Name, when, whenOK, clear, clearOK, now)
		if err != nil {
			log.Printf("rule %s device %s: saving alarms failed: %s\n", r.Name, obj.UID, err)
		}
		if ev == nil || (ev.Kind == "cleared" && !r.OnClear) {
			if ev != nil {
				log.Printf("rule %s device %s: cleared\n", r.Name, obj.UID)
			}
			continue
		}
		e.actions.Run(r, &Payload{
			Event: ev.Kind, Rule: r.Name, Device: obj.UID, Title: obj.Name,
			Namespace: obj.NamespaceName, Tags: obj.Tags, Alarm: ev.Alarm,
			Shadow: m, SentAt: now.UTC().Format(time.RFC3339Nano),
		})
	}
}
