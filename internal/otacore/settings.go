package otacore

import (
	"errors"
	"net/url"
	"strings"
)

// Settings is the addon process configuration. Token is the plugin bearer.
type Settings struct {
	Token         string
	Namespaces    []string
	StateFile     string
	APIURL        string
	AllowInsecure bool
}

// FromEnv reads OTA_ADDON_TOKEN, one of OTA_NAMESPACE or OTA_NAMESPACES,
// OTA_STATE_FILE, API_URL, and OTA_ALLOW_INSECURE.
func FromEnv(getenv func(string) string) (Settings, error) {
	var s Settings
	s.Token = strings.TrimSpace(getenv("OTA_ADDON_TOKEN"))
	if s.Token == "" {
		return s, errors.New("OTA_ADDON_TOKEN is required")
	}
	one := strings.TrimSpace(getenv("OTA_NAMESPACE"))
	many := strings.TrimSpace(getenv("OTA_NAMESPACES"))
	switch {
	case one != "" && many != "":
		return s, errors.New("set OTA_NAMESPACE or OTA_NAMESPACES")
	case one != "":
		s.Namespaces = []string{one}
	case many != "":
		for _, part := range strings.Split(many, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				return s, errors.New("OTA_NAMESPACES has an empty name")
			}
			s.Namespaces = append(s.Namespaces, part)
		}
	default:
		return s, errors.New("set OTA_NAMESPACE or OTA_NAMESPACES")
	}
	s.StateFile = strings.TrimSpace(getenv("OTA_STATE_FILE"))
	if s.StateFile == "" {
		return s, errors.New("OTA_STATE_FILE is required")
	}
	s.APIURL = strings.TrimSpace(getenv("API_URL"))
	if s.APIURL == "" {
		return s, errors.New("API_URL is required")
	}
	switch strings.TrimSpace(getenv("OTA_ALLOW_INSECURE")) {
	case "1", "true", "TRUE":
		s.AllowInsecure = true
	}
	return s, nil
}

// AllowURL accepts https, and http only when OTA_ALLOW_INSECURE is set.
func (s Settings) AllowURL(raw string) error {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New("url is not absolute")
	}
	if u.Scheme == "http" {
		if !s.AllowInsecure {
			return errors.New("http:// refused without OTA_ALLOW_INSECURE")
		}
		return nil
	}
	if u.Scheme != "https" {
		return errors.New("url scheme must be https")
	}
	return nil
}
