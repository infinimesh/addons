package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"
)

var statusSet = map[string]struct{}{
	"idle": {}, "downloading": {}, "installing": {}, "success": {}, "failed": {},
}

type inbound struct {
	Device   string
	Desired  map[string]any
	Reported map[string]any
}

func parseInbound(raw []byte) (inbound, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var top map[string]json.RawMessage
	if err := dec.Decode(&top); err != nil {
		return inbound{}, errors.New("invalid json")
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return inbound{}, errors.New("invalid json")
	}
	deviceRaw, ok := top["device"]
	if !ok {
		return inbound{}, errors.New("device is required")
	}
	var device string
	if err := json.Unmarshal(deviceRaw, &device); err != nil || device == "" {
		return inbound{}, errors.New("device is required")
	}
	var in inbound
	in.Device = device
	if raw, ok := top["target"]; ok {
		fields, err := objectFields(raw)
		if err != nil {
			return inbound{}, err
		}
		in.Desired, err = targetFields(fields)
		if err != nil {
			return inbound{}, err
		}
	}
	if raw, ok := top["report"]; ok {
		fields, err := objectFields(raw)
		if err != nil {
			return inbound{}, err
		}
		in.Reported, err = reportFields(fields)
		if err != nil {
			return inbound{}, err
		}
	}
	for key := range top {
		switch key {
		case "device", "target", "report":
		default:
			return inbound{}, errors.New("ota field " + key + " is not supported")
		}
	}
	if len(in.Desired) == 0 && len(in.Reported) == 0 {
		return inbound{}, errors.New("target or report is required")
	}
	return in, nil
}

func objectFields(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("ota must be an object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, errors.New("ota must be an object")
	}
	return fields, nil
}

func targetFields(fields map[string]json.RawMessage) (map[string]any, error) {
	out := map[string]any{}
	for key, raw := range fields {
		switch key {
		case "requested_version":
			return nil, errors.New("addon token can write desired ota only")
		case "version", "url":
			s, err := jsonString(key, raw)
			if err != nil {
				return nil, err
			}
			if s != "" {
				out[key] = s
			}
		case "sha256":
			s, err := jsonString(key, raw)
			if err != nil {
				return nil, err
			}
			if s != "" && !sha256Hex(s) {
				return nil, errors.New("ota sha256 must be 64 lowercase hex characters")
			}
			if s != "" {
				out[key] = s
			}
		case "size":
			n, err := jsonSize(raw)
			if err != nil {
				return nil, err
			}
			out[key] = float64(n)
		case "updated_at":
			s, err := jsonTime(raw)
			if err != nil {
				return nil, err
			}
			out[key] = s
		default:
			return nil, errors.New("ota field " + key + " is not supported")
		}
	}
	return out, nil
}

func reportFields(fields map[string]json.RawMessage) (map[string]any, error) {
	out := map[string]any{}
	for key, raw := range fields {
		switch key {
		case "requested_version":
			return nil, errors.New("addon token can write desired ota only")
		case "status":
			s, err := jsonString(key, raw)
			if err != nil {
				return nil, err
			}
			if _, ok := statusSet[s]; !ok {
				return nil, errors.New("ota status must be idle, downloading, installing, success, or failed")
			}
			out[key] = s
		case "version", "error":
			s, err := jsonString(key, raw)
			if err != nil {
				return nil, err
			}
			if key == "error" || s != "" {
				out[key] = s
			}
		case "updated_at":
			s, err := jsonTime(raw)
			if err != nil {
				return nil, err
			}
			out[key] = s
		default:
			return nil, errors.New("ota field " + key + " is not supported")
		}
	}
	return out, nil
}

func jsonString(key string, raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", errors.New("ota " + key + " must be a string")
	}
	return s, nil
}

func jsonSize(raw json.RawMessage) (int64, error) {
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, errors.New("ota size must be a whole number of zero or greater")
	}
	i, err := n.Int64()
	if err != nil || i < 0 {
		return 0, errors.New("ota size must be a whole number of zero or greater")
	}
	return i, nil
}

func jsonTime(raw json.RawMessage) (string, error) {
	s, err := jsonString("updated_at", raw)
	if err != nil {
		return "", err
	}
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		return "", errors.New("ota updated_at must be RFC3339")
	}
	return s, nil
}

func sha256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

func stamp(m map[string]any, now string) {
	if len(m) == 0 {
		return
	}
	if _, ok := m["updated_at"]; !ok {
		m["updated_at"] = now
	}
}
