package main

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/infinimesh/addons/internal/otacore"
)

type config struct {
	otacore.Settings
	Secret string
	URLs   map[string]string
	Listen string
	Poll   time.Duration
}

func load(getenv func(string) string) (config, error) {
	base, err := otacore.FromEnv(getenv)
	if err != nil {
		return config{}, err
	}
	c := config{Settings: base, URLs: map[string]string{}, Listen: strings.TrimSpace(getenv("OTA_WEBHOOK_LISTEN"))}
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	c.Secret = getenv("OTA_WEBHOOK_SECRET")
	if strings.TrimSpace(c.Secret) == "" {
		return config{}, errors.New("OTA_WEBHOOK_SECRET is required")
	}
	one := strings.TrimSpace(getenv("OTA_WEBHOOK_URL"))
	many := strings.TrimSpace(getenv("OTA_WEBHOOK_URLS"))
	switch {
	case one != "" && many != "":
		return config{}, errors.New("set OTA_WEBHOOK_URL or OTA_WEBHOOK_URLS")
	case one != "":
		if len(base.Namespaces) != 1 {
			return config{}, errors.New("set OTA_WEBHOOK_URLS")
		}
		c.URLs[base.Namespaces[0]] = one
	case many != "":
		for _, part := range strings.Split(many, ",") {
			part = strings.TrimSpace(part)
			ns, url, ok := strings.Cut(part, "=")
			ns = strings.TrimSpace(ns)
			url = strings.TrimSpace(url)
			if !ok || ns == "" || url == "" {
				return config{}, errors.New("OTA_WEBHOOK_URLS entries are namespace=url")
			}
			if _, dup := c.URLs[ns]; dup {
				return config{}, errors.New("OTA_WEBHOOK_URLS repeats " + ns)
			}
			c.URLs[ns] = url
		}
	}
	for _, ns := range base.Namespaces {
		url := c.URLs[ns]
		if url == "" {
			return config{}, errors.New("webhook_url is required when provider is webhook")
		}
		if err := base.AllowURL(url); err != nil {
			return config{}, err
		}
	}
	c.Poll, err = pollInterval(getenv("OTA_POLL_SECS"))
	if err != nil {
		return config{}, err
	}
	return c, nil
}

func pollInterval(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 15 * time.Second, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, errors.New("OTA_POLL_SECS must be a whole number of seconds")
	}
	return time.Duration(n) * time.Second, nil
}
