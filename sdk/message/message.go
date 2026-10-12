package message

import (
	"encoding/json"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Message is the JSON form of one shadow update that the addons send.
type Message struct {
	Device     string      `json:"device"`
	Title      string      `json:"title,omitempty"`
	Reported   *State      `json:"reported,omitempty"`
	Desired    *State      `json:"desired,omitempty"`
	Connection *Connection `json:"connection,omitempty"`
	SentAt     string      `json:"sent_at"`
}

type State struct {
	Timestamp string                 `json:"timestamp,omitempty"`
	Data      map[string]interface{} `json:"data"`
}

type Connection struct {
	Connected bool   `json:"connected"`
	Timestamp string `json:"timestamp,omitempty"`
	ClientID  string `json:"client_id,omitempty"`
}

func ts(t *timestamppb.Timestamp) string {
	if t == nil {
		return ""
	}
	return t.AsTime().UTC().Format(time.RFC3339Nano)
}

func state(s *shadowpb.State) *State {
	if s == nil || s.GetData() == nil {
		return nil
	}
	return &State{Timestamp: ts(s.GetTimestamp()), Data: s.GetData().AsMap()}
}

// Build turns a shadow update into a key (the device id) and a JSON value.
// ok is false when there is nothing to send.
func Build(obj api.Object, s *shadowpb.Shadow, onlyReported bool, now time.Time) (key, value []byte, ok bool) {
	m := Message{
		Device:   obj.UID,
		Title:    obj.Name,
		Reported: state(s.GetReported()),
		SentAt:   now.UTC().Format(time.RFC3339Nano),
	}
	if !onlyReported {
		m.Desired = state(s.GetDesired())
		if c := s.GetConnection(); c != nil {
			m.Connection = &Connection{Connected: c.GetConnected(), Timestamp: ts(c.GetTimestamp()), ClientID: c.GetClientId()}
		}
	}
	if m.Reported == nil && m.Desired == nil && m.Connection == nil {
		return nil, nil, false
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, nil, false
	}
	return []byte(obj.UID), b, true
}
