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
}

type handlerImpl struct {
	tokenHandler TokenHandler
	namespaces   pb.NamespacesServiceClient
	devices      pb.DevicesServiceClient
	shadow       pb.ShadowServiceClient
}

// NewHandler returns a Handler that talks to the infinimesh v3 API over gRPC.
func NewHandler(conn grpc.ClientConnInterface, tokenHandler TokenHandler) Handler {
	return &handlerImpl{
		tokenHandler: tokenHandler,
		namespaces:   pb.NewNamespacesServiceClient(conn),
		devices:      pb.NewDevicesServiceClient(conn),
		shadow:       pb.NewShadowServiceClient(conn),
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
	res, err := h.namespaces.List(ctx, &pb.EmptyMessage{})
	if err != nil {
		return nil, NewFailedRequestError("NamespacesService.List", err)
	}
	ret := &NamespacesRes{}
	for _, ns := range res.GetNamespaces() {
		ret.Namespaces = append(ret.Namespaces, Namespace{ID: ns.GetUuid(), Name: ns.GetTitle()})
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
}

func (h *handlerImpl) GetObjects(namespaceID string) (*ObjectsRes, error) {
	ctx, cancel := h.accountCtx()
	defer cancel()
	res, err := h.devices.List(ctx, &pb.QueryRequest{Namespace: namespaceID})
	if err != nil {
		return nil, NewFailedRequestError("DevicesService.List", err)
	}
	ret := &ObjectsRes{}
	for _, d := range res.GetDevices() {
		ret.Objects = append(ret.Objects, Object{UID: d.GetUuid(), Name: d.GetTitle(), Kind: "device"})
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
//
// It asks core for a short device token with read access to the device
// (DevicesService.MakeDevicesToken) and then calls ShadowService.StreamShadow
// with it. On any error it waits a second, gets a new token and reconnects.
func (h *handlerImpl) GetDevicesStateStream(deviceID string) (<-chan *DevicesStateRes, error) {
	ret := make(chan *DevicesStateRes)
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

func (h *handlerImpl) streamOnce(deviceID string, ch chan<- *DevicesStateRes) error {
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
		if msg.GetDevice() != "" && msg.GetDevice() != deviceID {
			continue
		}
		if st := ShadowToState(msg); st != nil {
			ch <- st
		}
	}
}
