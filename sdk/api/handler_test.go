package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type staticToken string

func (s staticToken) GetToken() string    { return string(s) }
func (s staticToken) RefreshToken() error { return nil }

func TestShadowStreamMessage(t *testing.T) {
	msg, ok := shadowStreamMessage([]byte(`{"result":{"reported":{"data":{"t":1},"timestamp":"2026-01-01T00:00:00Z","version":4}}}`))
	if !ok {
		t.Fatal("expected a reported message")
	}
	if msg.Result.ReportedState.Version != "4" || msg.Result.ReportedState.Timestamp != "2026-01-01T00:00:00Z" {
		t.Fatalf("state = %#v", msg.Result.ReportedState)
	}
	if msg.Result.ReportedState.Data["t"] != float64(1) {
		t.Fatalf("data = %#v", msg.Result.ReportedState.Data)
	}
	if _, ok := shadowStreamMessage([]byte(`{"result":{"event":"DEVICE_ADDED"}}`)); ok {
		t.Fatal("membership event has no reported state")
	}
}

func TestGetDevicesStateStreamUsesV1(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("authorization = %s", r.Header.Get("Authorization"))
		}
		got <- r.URL.RequestURI()
		_, _ = w.Write([]byte(`{"result":{"reported":{"data":{"t":1},"timestamp":"2026-01-01T00:00:00Z","version":4}}}`))
	}))
	t.Cleanup(srv.Close)

	h := &handlerImpl{
		client:       srv.Client(),
		apiUrl:       srv.URL,
		tokenHandler: staticToken("tok"),
	}
	ch, err := h.GetDevicesStateStream("dev/1")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case uri := <-got:
		want := "/v1/devices/states/stream?devices=dev%2F1&onlyDelta=false"
		if uri != want {
			t.Fatalf("uri = %s", uri)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream request was not sent")
	}
	select {
	case msg := <-ch:
		if msg.Result.ReportedState.Version != "4" {
			t.Fatalf("version = %s", msg.Result.ReportedState.Version)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream message was not delivered")
	}
}
