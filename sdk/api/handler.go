package api

import (
	"context"
	"errors"
	"io"
	"log"
	"time"

	pb "github.com/infinimesh/proto/node"
	"github.com/infinimesh/proto/node/access"
	shadowpb "github.com/infinimesh/proto/shadow"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Handler interface {
	GetNamespaces() (*NamespacesRes, error)
	GetObjects(namespaceID string) (*ObjectsRes, error)
	GetDevicesStateStream(deviceID string) (<-chan *DevicesStateRes, error)
	GetDeviceShadowStream(deviceID string) (<-chan *shadowpb.Shadow, error)
	StreamNamespace(ctx context.Context, namespaceID string, include []shadowpb.StateKey) <-chan *shadowpb.Shadow
}

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
	// Version is not part of the v3 shadow and is always empty. It stays so
	// existing sink tables keep their column.
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
		Timestamp: formatTimestamp(rep.GetTimestamp()),
		Data:      rep.GetData().AsMap(),
	}}}
}

func formatTimestamp(t *timestamppb.Timestamp) string {
	if t == nil {
		return time.Now().UTC().Format(time.RFC3339Nano)
	}
	return t.AsTime().UTC().Format(time.RFC3339Nano)
}

// GetDevicesStateStream streams reported state changes of one device.
// Shadow updates without reported data are skipped.
func (h *handlerImpl) GetDevicesStateStream(deviceID string) (<-chan *DevicesStateRes, error) {
	shadows, err := h.GetDeviceShadowStream(deviceID)
	if err != nil {
		return nil, err
	}
	ret := make(chan *DevicesStateRes)
	go func() {
		for s := range shadows {
			if st := ShadowToState(s); st != nil {
				ret <- st
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
// with it. On any error it waits a second, gets a new token and reconnects.
func (h *handlerImpl) GetDeviceShadowStream(deviceID string) (<-chan *shadowpb.Shadow, error) {
	ret := make(chan *shadowpb.Shadow)
	go func() {
		for {
			if err := h.streamOnce(deviceID, ret); err != nil {
				log.Printf("device %s state stream: %s\n", deviceID, err)
			}
			time.Sleep(time.Second)
		}
	}()
	return ret, nil
}

func (h *handlerImpl) streamOnce(deviceID string, ch chan<- *shadowpb.Shadow) error {
	tctx, cancel := h.accountCtx()
	tok, err := h.devices.MakeDevicesToken(tctx, &pb.DevicesTokenRequest{
		Devices: map[string]access.Level{deviceID: access.Level_READ},
	})
	cancel()
	if err != nil {
		return NewFailedRequestError("DevicesService.MakeDevicesToken", err)
	}

	ctx, stop := context.WithCancel(withBearer(context.Background(), tok.GetToken()))
	defer stop()
	stream, err := h.shadow.StreamShadow(ctx, &shadowpb.StreamShadowRequest{
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
		ch <- msg
	}
}

// StreamNamespace streams all devices of one namespace on one StreamShadow
// call and reconnects with resume_after. The channel closes when ctx is done.
func (h *handlerImpl) StreamNamespace(ctx context.Context, namespaceID string, include []shadowpb.StateKey) <-chan *shadowpb.Shadow {
	ret := make(chan *shadowpb.Shadow)
	go func() {
		defer close(ret)
		var last uint64
		for {
			if err := h.streamNamespaceOnce(ctx, namespaceID, include, &last, ret); err != nil && ctx.Err() == nil {
				log.Printf("namespace %s stream: %s\n", namespaceID, err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(h.retry):
			}
		}
	}()
	return ret
}

func (h *handlerImpl) streamNamespaceOnce(ctx context.Context, namespaceID string, include []shadowpb.StateKey, last *uint64, ch chan<- *shadowpb.Shadow) error {
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
	sctx, stop := context.WithCancel(withBearer(ctx, tok.GetToken()))
	defer stop()
	stream, err := h.shadow.StreamShadow(sctx, &shadowpb.StreamShadowRequest{
		Namespace:   namespaceID,
		Include:     include,
		ResumeAfter: *last,
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
