package api

import (
	"context"
	"errors"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	pb "github.com/infinimesh/proto/node"
	"github.com/infinimesh/proto/node/access"
	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Handler interface {
	GetNamespaces() (*NamespacesRes, error)
	GetObjects(namespaceID string) (*ObjectsRes, error)
	GetDevicesStateStream(ctx context.Context, deviceID string) (<-chan *DevicesStateRes, error)
	GetDeviceShadowStream(ctx context.Context, deviceID string) (<-chan *shadowpb.Shadow, error)
	StreamNamespace(ctx context.Context, namespaceID string, include []shadowpb.StateKey) <-chan *shadowpb.Shadow
	StreamNamespaceToken(ctx context.Context, namespaceID, token string, include []shadowpb.StateKey, resume uint64) (<-chan *shadowpb.Shadow, <-chan error)
}

// ErrTokenRevoked is the addon bearer after inf plugins rotate, a provider
// change, or plugin deletion.
var ErrTokenRevoked = errors.New("token is revoked; run inf plugins rotate and update the secret")

const streamTokenTTL = 24 * time.Hour

type handlerImpl struct {
	tokenHandler TokenHandler
	namespaces   pb.NamespacesServiceClient
	devices      pb.DevicesServiceClient
	shadow       pb.ShadowServiceClient
	retry        time.Duration
}

// NewHandler returns a Handler that talks to the infinimesh v3 API over gRPC.
func NewHandler(conn grpc.ClientConnInterface, tokenHandler TokenHandler) Handler {
	return &handlerImpl{
		tokenHandler: tokenHandler,
		namespaces:   pb.NewNamespacesServiceClient(conn),
		devices:      pb.NewDevicesServiceClient(conn),
		shadow:       pb.NewShadowServiceClient(conn),
		retry:        time.Second,
	}
}

func withBearer(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

func (h *handlerImpl) accountCtx() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	return withBearer(ctx, h.tokenHandler.GetToken()), cancel
}

type NamespacesRes struct {
	Namespaces []Namespace `json:"namespaces"`
}

type Namespace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (h *handlerImpl) GetNamespaces() (*NamespacesRes, error) {
	ctx, cancel := h.accountCtx()
	defer cancel()
	ret := &NamespacesRes{}
	req := &pb.ListRequest{}
	for {
		res, err := h.namespaces.List(ctx, req)
		if err != nil {
			return nil, NewFailedRequestError("NamespacesService.List", err)
		}
		for _, ns := range res.GetNamespaces() {
			ret.Namespaces = append(ret.Namespaces, Namespace{ID: ns.GetUuid(), Name: ns.GetTitle()})
		}
		// The API returns one page per call; follow the token to the end.
		if req.PageToken = res.GetNextPageToken(); req.PageToken == "" {
			break
		}
	}
	return ret, nil
}

type ObjectsRes struct {
	Objects []Object `json:"objects"`
}

// Object is a device the account can see. Kind is always "device" in v3.
type Object struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Tags are the device tags.
	Tags []string `json:"tags,omitempty"`
	// Namespace and NamespaceName are the namespace the device was listed in.
	Namespace     string `json:"namespace,omitempty"`
	NamespaceName string `json:"namespace_name,omitempty"`
}

func (h *handlerImpl) GetObjects(namespaceID string) (*ObjectsRes, error) {
	ctx, cancel := h.accountCtx()
	defer cancel()
	ret := &ObjectsRes{}
	req := &pb.QueryRequest{Namespace: namespaceID}
	for {
		res, err := h.devices.List(ctx, req)
		if err != nil {
			return nil, NewFailedRequestError("DevicesService.List", err)
		}
		for _, d := range res.GetDevices() {
			ret.Objects = append(ret.Objects, Object{UID: d.GetUuid(), Name: d.GetTitle(), Kind: "device", Tags: d.GetTags(), Namespace: namespaceID})
		}
		if req.PageToken = res.GetNextPageToken(); req.PageToken == "" {
			break
		}
	}
	return ret, nil
}

type DevicesStateRes struct {
	Result DevicesStateResult `json:"result"`
}

type DevicesStateResult struct {
	ReportedState DeviceState `json:"reportedState"`
}

type DeviceState struct {
	// Version is the reported state version from core as a decimal string,
	// empty when core sent none.
	Version   string                 `json:"version"`
	Timestamp string                 `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// ShadowToState converts a v3 shadow into the reported state the addons use.
// It returns nil when the shadow has no reported data.
func ShadowToState(s *shadowpb.Shadow) *DevicesStateRes {
	rep := s.GetReported()
	if rep == nil || rep.GetData() == nil {
		return nil
	}
	return &DevicesStateRes{Result: DevicesStateResult{ReportedState: DeviceState{
		Version:   formatVersion(rep.GetVersion()),
		Timestamp: formatTimestamp(rep.GetTimestamp()),
		Data:      rep.GetData().AsMap(),
	}}}
}

func formatVersion(v uint64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatUint(v, 10)
}

func formatTimestamp(t *timestamppb.Timestamp) string {
	if t == nil {
		return time.Now().UTC().Format(time.RFC3339Nano)
	}
	return t.AsTime().UTC().Format(time.RFC3339Nano)
}

// GetDevicesStateStream streams reported state changes of one device.
// Shadow updates without reported data are skipped. The stream stops and the
// channel closes when ctx is done.
func (h *handlerImpl) GetDevicesStateStream(ctx context.Context, deviceID string) (<-chan *DevicesStateRes, error) {
	shadows, err := h.GetDeviceShadowStream(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	ret := make(chan *DevicesStateRes)
	go func() {
		defer close(ret)
		for s := range shadows {
			st := ShadowToState(s)
			if st == nil {
				continue
			}
			select {
			case ret <- st:
			case <-ctx.Done():
			}
		}
	}()
	return ret, nil
}

// GetDeviceShadowStream streams full shadow updates (reported, desired and
// connection state) of one device.
//
// It asks core for a short device token with read access to the device
// (DevicesService.MakeDevicesToken) and then calls ShadowService.StreamShadow
// with it. On any error it waits, gets a new token and reconnects. The stream
// stops and the channel closes when ctx is done.
func (h *handlerImpl) GetDeviceShadowStream(ctx context.Context, deviceID string) (<-chan *shadowpb.Shadow, error) {
	ret := make(chan *shadowpb.Shadow)
	go func() {
		defer close(ret)
		for {
			if err := h.streamOnce(ctx, deviceID, ret); err != nil && ctx.Err() == nil {
				log.Printf("device %s state stream: %s\n", deviceID, err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(h.retry):
			}
		}
	}()
	return ret, nil
}

func (h *handlerImpl) streamOnce(ctx context.Context, deviceID string, ch chan<- *shadowpb.Shadow) error {
	tctx, cancel := h.accountCtx()
	tok, err := h.devices.MakeDevicesToken(tctx, &pb.DevicesTokenRequest{
		Devices: map[string]access.Level{deviceID: access.Level_READ},
	})
	cancel()
	if err != nil {
		return NewFailedRequestError("DevicesService.MakeDevicesToken", err)
	}

	sctx, stop := context.WithCancel(withBearer(ctx, tok.GetToken()))
	defer stop()
	stream, err := h.shadow.StreamShadow(sctx, &shadowpb.StreamShadowRequest{
		Devices:   []string{deviceID},
		OnlyDelta: false,
	})
	if err != nil {
		return NewFailedRequestError("ShadowService.StreamShadow", err)
	}
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return NewFailedRequestError("ShadowService.StreamShadow", err)
		}
		if msg.GetDevice() == "" {
			msg.Device = deviceID
		}
		if msg.GetDevice() != deviceID {
			continue
		}
		select {
		case ch <- msg:
		case <-ctx.Done():
			return nil
		}
	}
}

// StreamNamespace streams all devices of one namespace on one StreamShadow
// call and reconnects with resume_after. When core closes the stream because
// it fell behind, or the resume point is outside core's replay window, the
// events in between are lost; it then logs the gap as an error and reopens
// with sync so the current shadow of every device is sent again. The channel
// closes when ctx is done.
func (h *handlerImpl) StreamNamespace(ctx context.Context, namespaceID string, include []shadowpb.StateKey) <-chan *shadowpb.Shadow {
	ch, _ := h.openNamespace(ctx, namespaceID, include, false, "", 0, false)
	return ch
}

// StreamNamespaceToken is StreamNamespace authenticated with an OTA addon
// token. That token cannot call MakeDevicesToken, so the bearer is used as-is.
// A revoked token stops the stream and the error channel receives ErrTokenRevoked.
// resume is the last sequence from the state file. Zero asks for a sync of the
// current shadow, because a new process otherwise misses an ota:request that
// arrived while it was down.
func (h *handlerImpl) StreamNamespaceToken(ctx context.Context, namespaceID, token string, include []shadowpb.StateKey, resume uint64) (<-chan *shadowpb.Shadow, <-chan error) {
	return h.openNamespace(ctx, namespaceID, include, true, token, resume, true)
}

func (h *handlerImpl) openNamespace(ctx context.Context, namespaceID string, include []shadowpb.StateKey, addon bool, addonToken string, resume uint64, syncIfNew bool) (<-chan *shadowpb.Shadow, <-chan error) {
	ret := make(chan *shadowpb.Shadow)
	errc := make(chan error, 1)
	go func() {
		defer close(ret)
		defer close(errc)
		last := resume
		resync := syncIfNew && resume == 0
		for {
			err := h.streamNamespaceOnce(ctx, namespaceID, include, addon, addonToken, &last, &resync, ret)
			if err != nil && ctx.Err() == nil {
				if addon && isRevoked(err) {
					log.Printf("%s\n", ErrTokenRevoked.Error())
					errc <- ErrTokenRevoked
					return
				}
				if isGap(err) {
					resync = true
					log.Printf("ERROR namespace %s stream: events after sequence %d were lost (%s); reopening with sync to send the current state of every device\n", namespaceID, last, err)
				} else {
					log.Printf("namespace %s stream: %s\n", namespaceID, err)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(h.retry):
			}
		}
	}()
	return ret, errc
}

// gapTexts are the messages core uses when a stream lost events. Core sends
// them as code Unknown with the Aborted text inside for now, so the text is
// matched as well as the code.
var gapTexts = []string{"shadow stream fell behind", "resume window exceeded"}

// isGap reports whether err means the stream skipped events.
func isGap(err error) bool {
	if st, ok := status.FromError(err); ok && st.Code() == codes.Aborted {
		return true
	}
	msg := err.Error()
	for _, t := range gapTexts {
		if strings.Contains(msg, t) {
			return true
		}
	}
	return false
}

func isRevoked(err error) bool {
	return err != nil && strings.Contains(err.Error(), "token is revoked")
}

func (h *handlerImpl) streamNamespaceOnce(ctx context.Context, namespaceID string, include []shadowpb.StateKey, addon bool, addonToken string, last *uint64, resync *bool, ch chan<- *shadowpb.Shadow) error {
	var bearer string
	if addon {
		bearer = addonToken
	} else {
		tctx, cancel := h.accountCtx()
		tok, err := h.devices.MakeDevicesToken(tctx, &pb.DevicesTokenRequest{
			Namespace: namespaceID,
			Stream:    true,
			ExpiresAt: time.Now().Add(streamTokenTTL).Unix(),
		})
		cancel()
		if err != nil {
			return NewFailedRequestError("DevicesService.MakeDevicesToken", err)
		}
		bearer = tok.GetToken()
	}
	sctx, stop := context.WithCancel(withBearer(ctx, bearer))
	defer stop()
	req := &shadowpb.StreamShadowRequest{
		Namespace:   namespaceID,
		Include:     include,
		ResumeAfter: *last,
	}
	if *resync {
		// The gap cannot be replayed, so ask for the current state instead.
		req.Sync = true
		req.ResumeAfter = 0
	}
	stream, err := h.shadow.StreamShadow(sctx, req)
	if err != nil {
		return NewFailedRequestError("ShadowService.StreamShadow", err)
	}
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return NewFailedRequestError("ShadowService.StreamShadow", err)
		}
		// The sync is done once the stream delivers; until then a reconnect
		// asks for it again.
		*resync = false
		if msg.GetSequence() > *last {
			*last = msg.GetSequence()
		}
		select {
		case ch <- msg:
		case <-ctx.Done():
			return nil
		}
	}
}
