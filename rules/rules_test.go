package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/message"
)

const testRules = `
rules:
  - name: hot
    when: reported.temp > 80 && "pump" in tags
    clear: reported.temp < 75
    cooldown: 10m
    repeat: 30m
    on_clear: true
    actions:
      - log: {message: "too hot"}
      - webhook: {url: "${HOOK}", secret: "s3cret"}
      - mqtt: {topic: "alarms/{rule}/{device}", qos: 1}
      - desired: {fan: "on"}
  - name: offline
    when: "!connected"
`

type fakePub struct {
	mu     sync.Mutex
	topics []string
}

func (f *fakePub) Publish(topic string, _ byte, _ bool, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.topics = append(f.topics, topic)
	return nil
}

type fakeDesired struct{ calls []map[string]any }

func (f *fakeDesired) PatchDesired(_ string, d map[string]any) error {
	f.calls = append(f.calls, d)
	return nil
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{
		"rules: [{name: a}]",
		"rules: [{name: a, when: 'reported.x +'}]",
		"rules: [{name: a, when: '1 + 2'}]",
		"rules: [{name: a, when: 'true'}, {name: a, when: 'true'}]",
		"rules: [{name: 'a b', when: 'true'}]",
		"rules: [{name: a, when: 'true', actions: [{log: {}, mqtt: {topic: x}}]}]",
		"rules: [{name: a, when: 'true', actions: [{webhook: {url: ftp://x}}]}]",
		"rules: [{name: a, when: 'true', cooldown: soon}]",
	} {
		if _, err := Parse([]byte(bad), func(string) string { return "" }); err == nil {
			t.Errorf("no error for %s", bad)
		}
	}
}

func TestEngine(t *testing.T) {
	var mu sync.Mutex
	var events []Payload
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-Infinimesh-Signature") != sign("s3cret", b) {
			t.Error("bad signature")
		}
		var p Payload
		_ = json.Unmarshal(b, &p)
		mu.Lock()
		events = append(events, p)
		mu.Unlock()
	}))
	defer hook.Close()
	f, err := Parse([]byte(testRules), func(k string) string { return map[string]string{"HOOK": hook.URL}[k] })
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "alarms.json")
	store, _ := OpenStore(path)
	pub, des := &fakePub{}, &fakeDesired{}
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	e := &Engine{rules: f.Rules, store: store, now: func() time.Time { return now },
		actions: &Actions{client: hook.Client(), sleep: func(time.Duration) {}, mqtt: pub, desired: des}}
	obj := api.Object{UID: "d1", Name: "pump-7", Tags: []string{"pump"}}
	send := func(temp float64) {
		e.Check(obj, e.merge(&message.Message{Device: "d1",
			Reported:   &message.State{Data: map[string]any{"temp": temp}},
			Connection: &message.Connection{Connected: true}}))
	}
	kinds := func() []string {
		mu.Lock()
		defer mu.Unlock()
		var k []string
		for _, p := range events {
			k = append(k, p.Event)
		}
		return k
	}

	send(85) // raise
	send(90) // still raised, no new event
	now = now.Add(time.Minute)
	send(78) // between clear and when: stays raised
	send(70) // clear
	now = now.Add(time.Minute)
	send(85) // inside cooldown: nothing
	now = now.Add(10 * time.Minute)
	send(85) // after cooldown: raise again
	if got := kinds(); len(got) != 3 || got[0] != "raised" || got[1] != "cleared" || got[2] != "raised" {
		t.Fatalf("events %v", got)
	}
	if len(des.calls) != 2 || len(pub.topics) != 3 || pub.topics[0] != "alarms/hot/d1" {
		t.Fatalf("desired %v mqtt %v", des.calls, pub.topics)
	}
	if _, err := store.Ack("hot", "d1", now); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	send(85) // acked: no repeat
	if len(kinds()) != 3 {
		t.Fatalf("repeat after ack: %v", kinds())
	}

	// A restart keeps the alarm, so nothing fires again.
	store2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	e.store = store2
	send(86)
	if len(kinds()) != 3 {
		t.Fatalf("fired again after restart: %v", kinds())
	}
	a := store2.List()
	if len(a) != 1 || a[0].State != Acked || a[0].Count != 2 {
		t.Fatalf("alarms %+v", a)
	}
}

func TestRepeat(t *testing.T) {
	f, _ := Parse([]byte("rules: [{name: r, when: 'reported.x == 1', repeat: 5m}]"), func(string) string { return "" })
	s, _ := OpenStore("")
	now := time.Now()
	ev, _ := s.Step(f.Rules[0], "d", "", true, true, false, true, now)
	ev2, _ := s.Step(f.Rules[0], "d", "", true, true, false, true, now.Add(time.Minute))
	ev3, _ := s.Step(f.Rules[0], "d", "", true, true, false, true, now.Add(6*time.Minute))
	if ev == nil || ev2 != nil || ev3 == nil || ev3.Kind != "repeat" {
		t.Fatalf("%v %v %v", ev, ev2, ev3)
	}
}

func TestMissingFieldChangesNothing(t *testing.T) {
	f, _ := Parse([]byte("rules: [{name: r, when: 'reported.temp > 1'}]"), func(string) string { return "" })
	v := Vars{Reported: map[string]any{"hum": 3.0}}
	if _, ok := eval(f.Rules[0].when, v); ok {
		t.Fatal("missing field should be undecided")
	}
}

func TestAlarmAPI(t *testing.T) {
	s, _ := OpenStore("")
	r := &Rule{Name: "r"}
	_, _ = s.Step(r, "d1", "", true, true, false, true, time.Now())
	srv := httptest.NewServer(alarmAPI(s, "tok"))
	defer srv.Close()
	do := func(method, path, tok string) int {
		req, _ := http.NewRequest(method, srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if c := do("GET", "/alarms", "bad"); c != 401 {
		t.Fatal(c)
	}
	if c := do("POST", "/alarms/r/d1/ack", "tok"); c != 200 {
		t.Fatal(c)
	}
	if c := do("POST", "/alarms/r/d1/ack", "tok"); c != 409 {
		t.Fatal(c)
	}
	if c := do("GET", "/alarms?state=acked", "tok"); c != 200 {
		t.Fatal(c)
	}
}

func TestLoadSettings(t *testing.T) {
	old := config.Env
	t.Cleanup(func() { config.Env = old })
	env := map[string]string{"RULES_API_TOKEN": "x"}
	config.Env = func(k string) string { return env[k] }
	s, err := loadSettings()
	if err != nil || s.Retries != 5 || s.Backoff != 500*time.Millisecond {
		t.Fatal(s, err)
	}
	env = map[string]string{}
	if _, err := loadSettings(); err == nil {
		t.Fatal("api token should be required")
	}
	env = map[string]string{"RULES_HTTP_ADDR": "off"}
	if s, err := loadSettings(); err != nil || s.HTTPAddr != "" {
		t.Fatal("off should turn the api off", err)
	}
	env = map[string]string{"RULES_HTTP_ADDR": "off", "RULES_MQTT_BROKER": "http://x"}
	if _, err := loadSettings(); err == nil {
		t.Fatal("bad broker accepted")
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(testRules))
	f.Add([]byte("rules: [{name: a, when: 'true'}]"))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = Parse(b, func(string) string { return "" })
	})
}
