// Package config reads the settings the addons share from env vars.
package config

import (
	"crypto/tls"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
)

// Env is the env var lookup. Tests replace it.
var Env = os.Getenv

// Seconds reads the first of names that is set, as a number of seconds.
// It returns def when none is set.
func Seconds(def time.Duration, names ...string) (time.Duration, error) {
	for _, n := range names {
		v := strings.TrimSpace(Env(n))
		if v == "" {
			continue
		}
		s, err := strconv.Atoi(v)
		if err != nil || s <= 0 {
			return 0, fmt.Errorf("invalid %s value: %s", n, v)
		}
		return time.Duration(s) * time.Second, nil
	}
	return def, nil
}

// TokenRefresh reads TOKEN_REFRESH_INTERVAL_SECS, default 3600.
func TokenRefresh() (time.Duration, error) {
	return Seconds(time.Hour, "TOKEN_REFRESH_INTERVAL_SECS")
}

// DeviceRefresh reads DEVICE_REFRESH_INTERVAL_SECS, or the older name
// OBJECT_REFRESH_INTERVAL_SECS, default 600.
func DeviceRefresh() (time.Duration, error) {
	return Seconds(10*time.Minute, "DEVICE_REFRESH_INTERVAL_SECS", "OBJECT_REFRESH_INTERVAL_SECS")
}

// Bool reads a true or false env var, with a default when it is not set.
func Bool(name string, def bool) (bool, error) {
	v := strings.TrimSpace(Env(name))
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid %s value: %s", name, v)
	}
	return b, nil
}

// Int reads a whole number env var that is 0 or more, with a default.
func Int(name string, def int) (int, error) {
	v := strings.TrimSpace(Env(name))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid %s value: %s", name, v)
	}
	return n, nil
}

// String reads an env var with a default.
func String(name, def string) string {
	if v := strings.TrimSpace(Env(name)); v != "" {
		return v
	}
	return def
}

// RedisDialOptions reads REDIS_PASSWORD, REDIS_TLS and REDIS_TLS_INSECURE.
func RedisDialOptions() ([]redis.DialOption, error) {
	var opts []redis.DialOption
	if p := Env("REDIS_PASSWORD"); p != "" {
		opts = append(opts, redis.DialPassword(p))
	}
	useTLS, err := Bool("REDIS_TLS", false)
	if err != nil {
		return nil, err
	}
	insecure, err := Bool("REDIS_TLS_INSECURE", false)
	if err != nil {
		return nil, err
	}
	if useTLS {
		opts = append(opts, redis.DialUseTLS(true), redis.DialTLSSkipVerify(insecure), redis.DialTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}))
	}
	return opts, nil
}

// RedisPool returns a pool for REDIS_ADDR with the options above.
func RedisPool() (*redis.Pool, error) {
	addr := Env("REDIS_ADDR")
	if addr == "" {
		return nil, fmt.Errorf("REDIS_ADDR is required")
	}
	opts, err := RedisDialOptions()
	if err != nil {
		return nil, err
	}
	return &redis.Pool{Dial: func() (redis.Conn, error) {
		return redis.Dial("tcp", addr, opts...)
	}}, nil
}
