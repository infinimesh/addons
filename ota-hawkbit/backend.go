package main

import (
	"context"
	"errors"

	"github.com/infinimesh/addons/internal/otacore"
)

type backend struct {
	client *client
}

func (b *backend) Name() string { return "hawkbit" }

func (b *backend) EnsureDevice(ctx context.Context, d otacore.Device) error {
	name := d.Title
	if name == "" {
		name = d.ID
	}
	if err := b.client.ensureTarget(ctx, d.ID, name); err != nil {
		return down(err)
	}
	return down(b.client.setTag(ctx, "ns-"+d.Namespace, d.ID, true))
}

func (b *backend) SyncGroup(ctx context.Context, d otacore.Device, add, remove []string) error {
	for _, tag := range add {
		if _, ok := b.client.groups[tag]; !ok {
			continue
		}
		if err := b.client.setTag(ctx, "tag-"+tag, d.ID, true); err != nil {
			return down(err)
		}
	}
	for _, tag := range remove {
		if _, ok := b.client.groups[tag]; !ok {
			continue
		}
		if err := b.client.setTag(ctx, "tag-"+tag, d.ID, false); err != nil {
			return down(err)
		}
	}
	return nil
}

func (b *backend) Deploy(ctx context.Context, d otacore.Device, version string) error {
	ds, ok, err := b.client.findDist(ctx, version)
	if err != nil {
		return down(err)
	}
	if !ok {
		return &otacore.SkipDeploy{Msg: "unknown version " + version}
	}
	act, found, err := b.client.latestAction(ctx, d.ID)
	if err != nil {
		return down(err)
	}
	if found && act.DistID == ds.ID {
		return nil
	}
	return down(b.client.assign(ctx, d.ID, ds))
}

func (b *backend) Status(ctx context.Context, d otacore.Device) (otacore.Snapshot, error) {
	var snap otacore.Snapshot
	ds, ok, err := b.client.assigned(ctx, d.ID)
	if err != nil {
		return otacore.Snapshot{Error: "hawkbit unreachable"}, nil
	}
	if ok {
		snap.Version = ds.Name
	}
	act, found, err := b.client.latestAction(ctx, d.ID)
	if err != nil {
		return otacore.Snapshot{Error: "hawkbit unreachable"}, nil
	}
	if !found {
		if snap.Version == "" {
			snap.RawStatus = "SCHEDULED"
		}
		return snap, nil
	}
	snap.RawStatus = act.Status
	if m, ok := ActionStatus[act.Status]; ok {
		snap.Error = m.Error
	}
	return snap, nil
}

func (b *backend) Forget(ctx context.Context, d otacore.Device) error {
	return down(b.client.deleteTarget(ctx, d.ID))
}

func down(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &otacore.Unavailable{Msg: "hawkbit unreachable"}
}
