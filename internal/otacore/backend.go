package otacore

import "context"

// Unavailable is a backend that did not answer. The addon writes Msg on
// reported.ota.error and tries the same work again later.
type Unavailable struct{ Msg string }

func (e *Unavailable) Error() string {
	if e == nil || e.Msg == "" {
		return "unreachable"
	}
	return e.Msg
}

// Missing is a device the backend no longer has. The local ensured flag is stale.
type Missing struct{}

func (e *Missing) Error() string { return "target missing" }

// Backend is one OTA system. Ids and idempotency stay in the state file, not the shadow.
type Backend interface {
	Name() string
	EnsureDevice(ctx context.Context, d Device) error
	SyncGroup(ctx context.Context, d Device, add, remove []string) error
	Deploy(ctx context.Context, d Device, version string) error
	Status(ctx context.Context, d Device) (Snapshot, error)
	Forget(ctx context.Context, d Device) error
}

// Device is an infinimesh device in one namespace.
type Device struct {
	ID        string
	Namespace string
	Title     string
	Tags      []string
}

// Snapshot is the backend's view of one device, already mapped or still raw.
// RawStatus is mapped through the addon's status table before it is written.
type Snapshot struct {
	Version         string
	URL             string
	SHA256          string
	Size            *int64
	RawStatus       string
	ReportedVersion string
	Error           string
}
