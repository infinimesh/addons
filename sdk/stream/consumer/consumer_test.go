package consumer

import "testing"

func TestParseReply(t *testing.T) {
	reply := []interface{}{
		[]interface{}{
			[]byte("objects"),
			[]interface{}{
				[]interface{}{[]byte("1-0"), []interface{}{
					[]byte("object"), []byte(`{"uid":"d1","name":"pump","kind":"device"}`),
					[]byte("state"), []byte(`{"timestamp":"2026-10-12T08:00:00Z","data":{"temp":20}}`),
				}},
				// a pending entry that was deleted from the stream
				[]interface{}{[]byte("2-0"), nil},
			},
		},
	}
	events := parseReply(reply)
	if len(events) != 2 {
		t.Fatalf("events = %d", len(events))
	}
	e := events[0].DeviceEvent
	if e == nil || e.Object.UID != "d1" || e.State.Data["temp"] != 20.0 {
		t.Errorf("event = %+v", e)
	}
	if events[1].DeviceEvent != nil {
		t.Error("deleted entry should have no event")
	}
}
