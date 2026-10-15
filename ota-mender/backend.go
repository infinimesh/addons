package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"

	"github.com/infinimesh/addons/internal/otacore"
)

var tokenOnce sync.Once

const notRegistered = "not registered in mender"

type backend struct {
	client   *client
	groupTag string
	ids      map[string]string
	tags     map[string][]string
	group    map[string]string
	warned   map[string]bool
}

func newBackend(c *client, groupTag string) *backend {
	return &backend{
		client: c, groupTag: groupTag,
		ids: map[string]string{}, tags: map[string][]string{},
		group: map[string]string{}, warned: map[string]bool{},
	}
}

func (b *backend) Name() string { return "mender" }

func (b *backend) EnsureDevice(ctx context.Context, d otacore.Device) error {
	id, ok, err := b.client.findDevice(ctx, d.ID)
	if err != nil {
		return down(ctx, err)
	}
	if !ok {
		return &otacore.Report{Status: "idle", Msg: notRegistered}
	}
	b.ids[d.ID] = id
	b.tags[d.ID] = append([]string(nil), d.Tags...)
	return b.applyGroup(ctx, d)
}

func (b *backend) SyncGroup(ctx context.Context, d otacore.Device, add, remove []string) error {
	if _, ok := b.ids[d.ID]; !ok {
		return &otacore.Report{Status: "idle", Msg: notRegistered}
	}
	next := map[string]struct{}{}
	for _, tag := range b.tags[d.ID] {
		next[tag] = struct{}{}
	}
	for _, tag := range remove {
		delete(next, tag)
	}
	for _, tag := range add {
		if tag != "" {
			next[tag] = struct{}{}
		}
	}
	tags := make([]string, 0, len(next))
	for tag := range next {
		tags = append(tags, tag)
	}
	b.tags[d.ID] = tags
	return b.applyGroup(ctx, d)
}

func (b *backend) applyGroup(ctx context.Context, d otacore.Device) error {
	group, warn := pickGroup(d.Namespace, b.groupTag, b.tags[d.ID])
	if warn && !b.warned[d.ID] {
		b.warned[d.ID] = true
		log.Printf("device %s matches tag %s; mender allows one static group, keeping namespace group %s\n", d.ID, b.groupTag, group)
	}
	if b.group[d.ID] == group {
		return nil
	}
	if err := b.client.setGroup(ctx, b.ids[d.ID], group); err != nil {
		return down(ctx, err)
	}
	b.group[d.ID] = group
	return nil
}

func (b *backend) Deploy(ctx context.Context, d otacore.Device, version string) error {
	id, ok := b.ids[d.ID]
	if !ok {
		return &otacore.Missing{}
	}
	rows, err := b.deviceRows(ctx, id)
	if err != nil {
		return down(ctx, err)
	}
	if n, ok := newest(rows); ok && n.artifact == version && !badStatus(n.deviceStatus) {
		return nil
	}
	ok, err = b.client.hasArtifact(ctx, version)
	if err != nil {
		return down(ctx, err)
	}
	if !ok {
		return &otacore.SkipDeploy{Msg: "unknown version " + version}
	}
	for _, row := range rows {
		if row.open && row.artifact != version {
			if err := b.client.abort(ctx, row.id); err != nil {
				return down(ctx, err)
			}
		}
	}
	code, err := b.client.createDeployment(ctx, id, version)
	if err != nil {
		return down(ctx, err)
	}
	switch code {
	case http.StatusCreated, http.StatusConflict:
		return nil
	case http.StatusUnprocessableEntity:
		return &otacore.SkipDeploy{Msg: "unknown version " + version}
	default:
		return down(ctx, errors.New("mender deploy"))
	}
}

type deviceRow struct {
	id, artifact, created, deviceStatus string
	open                                bool
}

func (b *backend) deviceRows(ctx context.Context, menderID string) ([]deviceRow, error) {
	hist, err := b.client.history(ctx, menderID)
	if err != nil {
		return nil, err
	}
	opens, err := b.client.openFor(ctx, menderID)
	if err != nil {
		return nil, err
	}
	var rows []deviceRow
	seenID := map[string]struct{}{}
	for _, row := range hist {
		rows = append(rows, deviceRow{
			id: row.Deployment.ID, artifact: row.Deployment.ArtifactName,
			created: row.Deployment.Created, deviceStatus: row.Device.Status,
		})
		if row.Deployment.ID != "" {
			seenID[row.Deployment.ID] = struct{}{}
		}
	}
	for _, row := range opens {
		if _, ok := seenID[row.ID]; ok {
			continue
		}
		rows = append(rows, deviceRow{
			id: row.ID, artifact: row.ArtifactName, created: row.Created,
			deviceStatus: "pending", open: true,
		})
	}
	return rows, nil
}

func badStatus(status string) bool {
	switch status {
	case "failure", "aborted", "noartifact", "decommissioned":
		return true
	default:
		return false
	}
}

func newest(rows []deviceRow) (deviceRow, bool) {
	var best deviceRow
	var have bool
	for _, row := range rows {
		if !have || row.created > best.created {
			best = row
			have = true
		}
	}
	return best, have
}

func (b *backend) Status(ctx context.Context, d otacore.Device) (otacore.Snapshot, error) {
	id, ok := b.ids[d.ID]
	if !ok {
		found, ok, err := b.client.findDevice(ctx, d.ID)
		if err != nil {
			return otacore.Snapshot{Error: down(ctx, err).Error()}, nil
		}
		if !ok {
			return otacore.Snapshot{RawStatus: "UNREGISTERED"}, nil
		}
		id = found
		b.ids[d.ID] = id
	}
	rows, err := b.deviceRows(ctx, id)
	if err != nil {
		return otacore.Snapshot{Error: down(ctx, err).Error()}, nil
	}
	row, ok := newest(rows)
	if !ok {
		return otacore.Snapshot{RawStatus: "pending"}, nil
	}
	snap := otacore.Snapshot{Version: row.artifact, RawStatus: row.deviceStatus}
	switch row.deviceStatus {
	case "success", "already-installed":
		snap.ReportedVersion = row.artifact
	}
	if m, ok := otacore.MapStatus(DeviceStatus, snap.RawStatus); ok {
		snap.Error = m.Error
	}
	return snap, nil
}

func (b *backend) Forget(ctx context.Context, d otacore.Device) error {
	id := b.ids[d.ID]
	group := b.group[d.ID]
	if id != "" && group != "" {
		if err := b.client.clearGroup(ctx, id, group); err != nil {
			return down(ctx, err)
		}
	}
	delete(b.ids, d.ID)
	delete(b.tags, d.ID)
	delete(b.group, d.ID)
	delete(b.warned, d.ID)
	return nil
}

func down(ctx context.Context, err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
		return err
	}
	if errors.Is(err, errToken) {
		tokenOnce.Do(func() { log.Printf("mender rejected the token\n") })
		return &otacore.Unavailable{Msg: "mender token rejected"}
	}
	return &otacore.Unavailable{Msg: "mender unreachable"}
}
