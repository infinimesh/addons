package main

import (
	"crypto/tls"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kversion"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

type config struct {
	Username          string
	Password          string
	ApiUrl            string
	TokenRefresh      time.Duration
	DeviceRefresh     time.Duration
	Brokers           []string
	Topic             string
	ClientID          string
	TLS               bool
	TLSInsecure       bool
	SASLMechanism     string
	SASLUsername      string
	SASLPassword      string
	Idempotent        bool
	CreateTopic       bool
	Partitions        int32
	ReplicationFactor int16
	OnlyReported      bool
}

func getenv(env func(string) string, key, def string) string {
	if v := strings.TrimSpace(env(key)); v != "" {
		return v
	}
	return def
}

func loadConfig(env func(string) string) (*config, error) {
	c := &config{
		Username:      env("USERNAME"),
		Password:      env("PASSWORD"),
		ApiUrl:        env("API_URL"),
		Topic:         getenv(env, "KAFKA_TOPIC", "infinimesh.shadows"),
		ClientID:      getenv(env, "KAFKA_CLIENT_ID", "infinimesh-kafka-addon"),
		SASLMechanism: strings.ToUpper(strings.TrimSpace(env("KAFKA_SASL_MECHANISM"))),
		SASLUsername:  env("KAFKA_SASL_USERNAME"),
		SASLPassword:  env("KAFKA_SASL_PASSWORD"),
	}
	if c.ApiUrl == "" {
		return nil, fmt.Errorf("API_URL is required")
	}
	for _, b := range strings.Split(env("KAFKA_BROKERS"), ",") {
		if b = strings.TrimSpace(b); b != "" {
			c.Brokers = append(c.Brokers, b)
		}
	}
	if len(c.Brokers) == 0 {
		return nil, fmt.Errorf("KAFKA_BROKERS is required, for example kafka:9092")
	}

	var err error
	secs := func(key, def string) (time.Duration, error) {
		v := getenv(env, key, def)
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid %s value: %s", key, v)
		}
		return time.Duration(n) * time.Second, nil
	}
	if c.TokenRefresh, err = secs("TOKEN_REFRESH_INTERVAL_SECS", "3600"); err != nil {
		return nil, err
	}
	if c.DeviceRefresh, err = secs("DEVICE_REFRESH_INTERVAL_SECS", "600"); err != nil {
		return nil, err
	}
	flag := func(key string, def bool) (bool, error) {
		v := strings.TrimSpace(env(key))
		if v == "" {
			return def, nil
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			return false, fmt.Errorf("invalid %s value: %s", key, v)
		}
		return b, nil
	}
	if c.TLS, err = flag("KAFKA_TLS", false); err != nil {
		return nil, err
	}
	if c.TLSInsecure, err = flag("KAFKA_TLS_INSECURE", false); err != nil {
		return nil, err
	}
	if c.Idempotent, err = flag("KAFKA_IDEMPOTENT", false); err != nil {
		return nil, err
	}
	if c.CreateTopic, err = flag("KAFKA_CREATE_TOPIC", true); err != nil {
		return nil, err
	}
	if c.OnlyReported, err = flag("KAFKA_ONLY_REPORTED", false); err != nil {
		return nil, err
	}
	p, err := strconv.ParseInt(getenv(env, "KAFKA_PARTITIONS", "1"), 10, 32)
	if err != nil || p <= 0 {
		return nil, fmt.Errorf("invalid KAFKA_PARTITIONS value")
	}
	c.Partitions = int32(p)
	r, err := strconv.ParseInt(getenv(env, "KAFKA_REPLICATION_FACTOR", "1"), 10, 16)
	if err != nil || r <= 0 {
		return nil, fmt.Errorf("invalid KAFKA_REPLICATION_FACTOR value")
	}
	c.ReplicationFactor = int16(r)

	switch c.SASLMechanism {
	case "", "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512":
	default:
		return nil, fmt.Errorf("unsupported KAFKA_SASL_MECHANISM %q, use PLAIN, SCRAM-SHA-256 or SCRAM-SHA-512", c.SASLMechanism)
	}
	return c, nil
}

func (c *config) kafkaOpts() []kgo.Opt {
	opts := []kgo.Opt{
		kgo.SeedBrokers(c.Brokers...),
		kgo.ClientID(c.ClientID),
		kgo.DefaultProduceTopic(c.Topic),
		kgo.ProducerBatchCompression(kgo.NoCompression()),
		// Cap request versions at Kafka 3.9. Newer franz-go sends ApiVersions
		// v5 first, and KafScale closes the connection instead of answering
		// with UNSUPPORTED_VERSION. A producer needs nothing from Kafka 4.x.
		kgo.MaxVersions(kversion.V3_9_0()),
	}
	if !c.Idempotent {
		// KafScale has no InitProducerId, so idempotent writes must be off.
		// Plain Kafka works either way.
		opts = append(opts, kgo.DisableIdempotentWrite())
	}
	if c.TLS {
		opts = append(opts, kgo.DialTLSConfig(&tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: c.TLSInsecure, // #nosec G402 -- opt in, for test clusters only
		}))
	}
	if m := c.saslMechanism(); m != nil {
		opts = append(opts, kgo.SASL(m))
	}
	return opts
}

func (c *config) saslMechanism() sasl.Mechanism {
	switch c.SASLMechanism {
	case "PLAIN":
		return plain.Auth{User: c.SASLUsername, Pass: c.SASLPassword}.AsMechanism()
	case "SCRAM-SHA-256":
		return scram.Auth{User: c.SASLUsername, Pass: c.SASLPassword}.AsSha256Mechanism()
	case "SCRAM-SHA-512":
		return scram.Auth{User: c.SASLUsername, Pass: c.SASLPassword}.AsSha512Mechanism()
	}
	return nil
}
