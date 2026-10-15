package otacore

import (
	"context"
	"errors"

	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/protobuf/types/known/structpb"
)

// Fields is one ota object. RequestedVersion is never written.
type Fields struct {
	Version   string
	URL       string
	SHA256    string
	Size      *int64
	Status    string
	Error     string
	ErrorSet  bool
	UpdatedAt string
}

// Patch is one device state write. Desired and Reported hold only ota fields.
type Patch struct {
	Device   string
	Desired  map[string]any
	Reported map[string]any
}

// Writer sends a patch with the addon token.
type Writer interface {
	Patch(ctx context.Context, p Patch) error
}

var (
	desiredKeys = map[string]struct{}{
		"version": {}, "url": {}, "sha256": {}, "size": {}, "updated_at": {},
	}
	reportedKeys = map[string]struct{}{
		"status": {}, "version": {}, "error": {}, "updated_at": {},
	}
)

func desiredFields(f Fields) map[string]any {
	m := map[string]any{}
	putString(m, "version", f.Version)
	putString(m, "url", f.URL)
	putString(m, "sha256", f.SHA256)
	if f.Size != nil {
		m["size"] = float64(*f.Size)
	}
	if len(m) > 0 {
		putString(m, "updated_at", f.UpdatedAt)
	}
	return m
}

func reportedFields(f Fields) map[string]any {
	m := map[string]any{}
	putString(m, "status", f.Status)
	putString(m, "version", f.Version)
	if f.ErrorSet {
		m["error"] = f.Error
	}
	if len(m) > 0 {
		putString(m, "updated_at", f.UpdatedAt)
	}
	return m
}

func putString(m map[string]any, key, val string) {
	if val != "" {
		m[key] = val
	}
}

func checkKeys(m map[string]any, allowed map[string]struct{}) error {
	for k := range m {
		if _, ok := allowed[k]; !ok {
			return errors.New("ota field " + k + " is not supported")
		}
	}
	return nil
}

func shadowFrom(p Patch) (*shadowpb.Shadow, error) {
	if err := checkKeys(p.Desired, desiredKeys); err != nil {
		return nil, err
	}
	if err := checkKeys(p.Reported, reportedKeys); err != nil {
		return nil, err
	}
	out := &shadowpb.Shadow{Device: p.Device}
	var err error
	if len(p.Desired) > 0 {
		if out.Desired, err = otaState(p.Desired); err != nil {
			return nil, err
		}
	}
	if len(p.Reported) > 0 {
		if out.Reported, err = otaState(p.Reported); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func otaState(fields map[string]any) (*shadowpb.State, error) {
	ota, err := structpb.NewStruct(fields)
	if err != nil {
		return nil, err
	}
	data, err := structpb.NewStruct(map[string]any{})
	if err != nil {
		return nil, err
	}
	data.Fields["ota"] = structpb.NewStructValue(ota)
	return &shadowpb.State{Data: data}, nil
}
