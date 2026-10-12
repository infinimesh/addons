package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/infinimesh/addons/sdk/config"
)

type settings struct {
	Broker       string
	ClientID     string
	Username     string
	Password     string
	Topic        string
	QoS          byte
	Retain       bool
	Timeout      time.Duration
	OnlyReported bool
	TLS          *tls.Config
}

const defaultTopic = "infinimesh/devices/{device}/shadow"

func loadSettings() (*settings, error) {
	s := &settings{
		Broker:   config.Env("MQTT_BROKER"),
		ClientID: config.String("MQTT_CLIENT_ID", "infinimesh-mqttbridge"),
		Username: config.Env("MQTT_USERNAME"),
		Password: config.Env("MQTT_PASSWORD"),
		Topic:    config.String("MQTT_TOPIC", defaultTopic),
	}
	u, err := url.Parse(s.Broker)
	if s.Broker == "" || err != nil || u.Host == "" {
		return nil, fmt.Errorf("MQTT_BROKER must be a URL such as tcp://broker:1883 or ssl://broker:8883")
	}
	secure := false
	switch u.Scheme {
	case "tcp", "mqtt", "ws":
	case "ssl", "tls", "mqtts", "wss":
		secure = true
	default:
		return nil, fmt.Errorf("MQTT_BROKER scheme %q is not supported", u.Scheme)
	}
	if err := checkTopic(s.Topic); err != nil {
		return nil, err
	}
	qos, err := config.Int("MQTT_QOS", 1)
	if err != nil {
		return nil, err
	}
	if qos < 0 || qos > 2 {
		return nil, fmt.Errorf("MQTT_QOS must be 0, 1 or 2")
	}
	s.QoS = byte(qos)
	if s.Retain, err = config.Bool("MQTT_RETAIN", false); err != nil {
		return nil, err
	}
	if s.Timeout, err = config.Seconds(10*time.Second, "MQTT_TIMEOUT_SECS"); err != nil {
		return nil, err
	}
	if s.OnlyReported, err = config.Bool("MQTT_ONLY_REPORTED", false); err != nil {
		return nil, err
	}
	if secure {
		if s.TLS, err = tlsConfig(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func tlsConfig() (*tls.Config, error) {
	c := &tls.Config{MinVersion: tls.VersionTLS12}
	insecure, err := config.Bool("MQTT_TLS_INSECURE", false)
	if err != nil {
		return nil, err
	}
	c.InsecureSkipVerify = insecure
	if f := config.Env("MQTT_TLS_CA_FILE"); f != "" {
		pem, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("MQTT_TLS_CA_FILE: %w", err)
		}
		c.RootCAs = x509.NewCertPool()
		if !c.RootCAs.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("MQTT_TLS_CA_FILE has no PEM certificates")
		}
	}
	cert, key := config.Env("MQTT_TLS_CERT_FILE"), config.Env("MQTT_TLS_KEY_FILE")
	if (cert == "") != (key == "") {
		return nil, fmt.Errorf("set both MQTT_TLS_CERT_FILE and MQTT_TLS_KEY_FILE, or neither")
	}
	if cert != "" {
		pair, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, fmt.Errorf("client certificate: %w", err)
		}
		c.Certificates = []tls.Certificate{pair}
	}
	return c, nil
}

// checkTopic makes sure the template only has known placeholders and no
// wildcards.
func checkTopic(t string) error {
	rest := strings.NewReplacer("{device}", "", "{title}", "").Replace(t)
	if t == "" || strings.ContainsAny(rest, "{}+#") {
		return fmt.Errorf("MQTT_TOPIC may use {device} and {title}, and no + or # wildcards")
	}
	return nil
}

// topicPart makes a value safe to put in one topic level.
func topicPart(v string) string {
	return strings.NewReplacer("/", "_", "+", "_", "#", "_").Replace(v)
}

// topic fills in the template.
func topic(tmpl, device, title string) string {
	return strings.NewReplacer(
		"{device}", topicPart(device),
		"{title}", topicPart(title),
	).Replace(tmpl)
}
