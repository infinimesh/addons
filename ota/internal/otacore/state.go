package otacore

import (
	"encoding/json"
	"os"
	"sync"
)

type devState struct {
	Namespace string   `json:"namespace"`
	Tags      []string `json:"tags,omitempty"`
	Ensured   bool     `json:"ensured"`
	Deployed  string   `json:"deployed,omitempty"`
	Status    string   `json:"status,omitempty"`
	Version   string   `json:"version,omitempty"`
	Error     string   `json:"error,omitempty"`
	ErrorSet  bool     `json:"error_set,omitempty"`
	Desired   string   `json:"desired,omitempty"`
	URL       string   `json:"url,omitempty"`
	SHA256    string   `json:"sha256,omitempty"`
	Size      *int64   `json:"size,omitempty"`
	SizeSet   bool     `json:"size_set,omitempty"`
}

type disk struct {
	Devices map[string]devState `json:"devices"`
	Quiet   map[string]string   `json:"quiet,omitempty"`
}

type store struct {
	mu      sync.Mutex
	path    string
	Devices map[string]devState
	Quiet   map[string]string
}

func loadStore(path string) (*store, error) {
	s := &store{path: path, Devices: map[string]devState{}, Quiet: map[string]string{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var d disk
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	if d.Devices != nil {
		s.Devices = d.Devices
	}
	if d.Quiet != nil {
		s.Quiet = d.Quiet
	}
	return s, nil
}

func (s *store) quiet(ns string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Quiet[ns] != ""
}

func (s *store) markQuiet(ns, msg string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Quiet[ns] != "" {
		return false
	}
	s.Quiet[ns] = msg
	_ = s.saveLocked()
	return true
}

func (s *store) get(key string) (devState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.Devices[key]
	if !ok {
		return devState{}, false
	}
	d.Tags = append([]string(nil), d.Tags...)
	return d, true
}

func (s *store) put(key string, d devState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d.Tags = append([]string(nil), d.Tags...)
	s.Devices[key] = d
	_ = s.saveLocked()
}

func (s *store) del(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Devices, key)
	_ = s.saveLocked()
}

func (s *store) saveLocked() error {
	d := disk{Devices: s.Devices, Quiet: s.Quiet}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
