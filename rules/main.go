package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/runner"
	pb "github.com/infinimesh/proto/node"
	"github.com/infinimesh/proto/node/access"
	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"
)

type settings struct {
	File       string
	StateFile  string
	HTTPAddr   string
	APIToken   string
	Timeout    time.Duration
	Retries    int
	Backoff    time.Duration
	MaxBackoff time.Duration
	Broker     string
	MQTTUser   string
	MQTTPass   string
	MQTTClient string
}

func loadSettings() (*settings, error) {
	s := &settings{
		File:       config.String("RULES_FILE", "/etc/infinimesh/rules.yaml"),
		StateFile:  config.String("RULES_STATE_FILE", "/var/lib/infinimesh-rules/alarms.json"),
		HTTPAddr:   config.String("RULES_HTTP_ADDR", ":8080"),
		APIToken:   config.Env("RULES_API_TOKEN"),
		Broker:     config.Env("RULES_MQTT_BROKER"),
		MQTTUser:   config.Env("RULES_MQTT_USERNAME"),
		MQTTPass:   config.Env("RULES_MQTT_PASSWORD"),
		MQTTClient: config.String("RULES_MQTT_CLIENT_ID", "infinimesh-rules"),
	}
	if s.HTTPAddr == "off" {
		s.HTTPAddr = ""
	}
	var err error
	if s.Timeout, err = config.Seconds(10*time.Second, "RULES_WEBHOOK_TIMEOUT_SECS"); err != nil {
		return nil, err
	}
	if s.Retries, err = config.Int("RULES_WEBHOOK_MAX_RETRIES", 5); err != nil {
		return nil, err
	}
	ms, err := config.Int("RULES_WEBHOOK_BACKOFF_MS", 500)
	if err != nil {
		return nil, err
	}
	s.Backoff = time.Duration(ms) * time.Millisecond
	if s.MaxBackoff, err = config.Seconds(30*time.Second, "RULES_WEBHOOK_MAX_BACKOFF_SECS"); err != nil {
		return nil, err
	}
	if s.Broker != "" {
		u, err := url.Parse(s.Broker)
		if err != nil || (u.Scheme != "tcp" && u.Scheme != "ssl" && u.Scheme != "ws" && u.Scheme != "wss") {
			return nil, fmt.Errorf("RULES_MQTT_BROKER must be a URL such as tcp://broker:1883 or ssl://broker:8883")
		}
	}
	if s.HTTPAddr != "" && s.APIToken == "" {
		return nil, fmt.Errorf("RULES_API_TOKEN is required for the alarm API, or set RULES_HTTP_ADDR=off to turn the API off")
	}
	return s, nil
}

type mqttPublisher struct {
	c       mqtt.Client
	timeout time.Duration
}

func (p *mqttPublisher) Publish(topic string, qos byte, retain bool, body []byte) error {
	t := p.c.Publish(topic, qos, retain, body)
	if !t.WaitTimeout(p.timeout) {
		return errors.New("mqtt publish timed out")
	}
	return t.Error()
}

// patcher sets desired state through the shadow API, with a short lived
// MGMT token for that one device.
type patcher struct {
	tokens  api.TokenHandler
	devices pb.DevicesServiceClient
	shadow  pb.ShadowServiceClient
}

func (p *patcher) PatchDesired(device string, data map[string]any) error {
	st, err := structpb.NewStruct(data)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tok, err := p.devices.MakeDevicesToken(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+p.tokens.GetToken()),
		&pb.DevicesTokenRequest{Devices: map[string]access.Level{device: access.Level_MGMT}})
	if err != nil {
		return fmt.Errorf("device token: %w", err)
	}
	_, err = p.shadow.Patch(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+tok.GetToken()),
		&shadowpb.Shadow{Device: device, Desired: &shadowpb.State{Data: st}})
	return err
}

func alarmAPI(store *Store, token string) http.Handler {
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /alarms", auth(func(w http.ResponseWriter, r *http.Request) {
		list := store.List()
		if st := r.URL.Query().Get("state"); st != "" {
			out := list[:0]
			for _, a := range list {
				if a.State == st {
					out = append(out, a)
				}
			}
			list = out
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}))
	mux.HandleFunc("POST /alarms/{rule}/{device}/ack", auth(func(w http.ResponseWriter, r *http.Request) {
		a, err := store.Ack(r.PathValue("rule"), r.PathValue("device"), time.Now())
		if errors.Is(err, errNotRaised) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(w, "saving failed", http.StatusInternalServerError)
			return
		}
		log.Printf("rule %s device %s: acked\n", a.Rule, a.Device)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(a)
	}))
	return mux
}

func main() {
	s, err := loadSettings()
	if err != nil {
		log.Fatal(err)
	}
	b, err := os.ReadFile(s.File)
	if err != nil {
		log.Fatal(err)
	}
	f, err := Parse(b, os.Getenv)
	if err != nil {
		log.Fatalf("%s: %s", s.File, err)
	}
	store, err := OpenStore(s.StateFile)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("loaded %d rules, %d alarms\n", len(f.Rules), len(store.List()))

	tokenRefresh, err := config.TokenRefresh()
	if err != nil {
		log.Fatal(err)
	}
	conn, err := api.Dial(config.Env("API_URL"))
	if err != nil {
		log.Fatal(err)
	}
	tokens := api.NewTokenHandler(conn, config.Env("USERNAME"), config.Env("PASSWORD"), tokenRefresh)
	acts := &Actions{
		client: &http.Client{Timeout: s.Timeout}, retries: s.Retries, backoff: s.Backoff,
		maxBackoff: s.MaxBackoff, sleep: time.Sleep,
		desired: &patcher{tokens: tokens, devices: pb.NewDevicesServiceClient(conn), shadow: pb.NewShadowServiceClient(conn)},
	}
	if s.Broker != "" {
		o := mqtt.NewClientOptions().AddBroker(s.Broker).SetClientID(s.MQTTClient).
			SetUsername(s.MQTTUser).SetPassword(s.MQTTPass).SetAutoReconnect(true).SetConnectRetry(true)
		c := mqtt.NewClient(o)
		c.Connect() // connects in the background and retries
		acts.mqtt = &mqttPublisher{c: c, timeout: s.Timeout}
	}
	if s.HTTPAddr != "" {
		srv := &http.Server{Addr: s.HTTPAddr, Handler: alarmAPI(store, s.APIToken), ReadHeaderTimeout: 10 * time.Second}
		go func() { log.Fatal(srv.ListenAndServe()) }()
	}
	e := &Engine{rules: f.Rules, store: store, actions: acts, now: time.Now}
	runner.Run(e.Handle)
}
