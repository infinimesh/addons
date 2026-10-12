package consumer

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/config"
)

const (
	streamName = "objects"
	groupName  = "group"
)

type RedisStreamEvent struct {
	ID          string
	DeviceEvent *DeviceEvent
}

type DeviceEvent struct {
	Object api.Object
	State  api.DeviceState
}

// Handler writes one event to a sink. An entry is acknowledged only when the
// handler returns nil, so a failed entry is read again later.
type Handler func(*DeviceEvent) error

type Consumer interface {
	Run(h Handler)
}

type consumerImpl struct {
	redis   *redis.Pool
	name    string
	startID string
}

// New returns a consumer of the stream "objects" in the group "group".
//
// REDIS_STREAM_START_ID sets where the group starts when it is first created:
// "$" (the default) for new entries only, or "0" for the whole stream.
func New(pool *redis.Pool) Consumer {
	hn, _ := os.Hostname()
	return &consumerImpl{
		redis:   pool,
		name:    hn,
		startID: config.String("REDIS_STREAM_START_ID", "$"),
	}
}

// Run reads the stream forever. It first works through entries this consumer
// read before but did not acknowledge, then reads new ones.
func (i *consumerImpl) Run(h Handler) {
	pending := true
	for {
		id := ">"
		if pending {
			id = "0"
		}
		n, failed, err := i.loop(id, h)
		if err != nil {
			log.Printf("error on XREADGROUP: %v\n", err)
			// XREADGROUP fails when the group does not exist yet
			i.createGroupIfNotExists()
			time.Sleep(time.Second)
			continue
		}
		switch {
		case failed > 0:
			// keep retrying the pending entries, but not in a tight loop
			pending = true
			time.Sleep(time.Second)
		case pending && n == 0:
			pending = false
		case !pending && n == 0:
			time.Sleep(time.Second)
		}
	}
}

func (i *consumerImpl) loop(id string, h Handler) (n, failed int, err error) {
	conn := i.redis.Get()
	defer conn.Close()
	reply, err := conn.Do("XREADGROUP", "GROUP", groupName, i.name, "COUNT", 100, "STREAMS", streamName, id)
	if err != nil {
		return 0, 0, err
	}
	if reply == nil {
		return 0, 0, nil
	}
	for _, e := range parseReply(reply) {
		n++
		if e.DeviceEvent == nil {
			// unreadable entry, acknowledge it so it does not block the stream
			ack(conn, e.ID)
			continue
		}
		if err := h(e.DeviceEvent); err != nil {
			log.Printf("sink write failed for entry %s, will retry: %v\n", e.ID, err)
			failed++
			continue
		}
		ack(conn, e.ID)
	}
	return n, failed, nil
}

func ack(conn redis.Conn, id string) {
	if _, err := conn.Do("XACK", streamName, groupName, id); err != nil {
		log.Printf("error on XACK: %v\n", err)
	}
}

func (i *consumerImpl) createGroupIfNotExists() {
	conn := i.redis.Get()
	defer conn.Close()
	_, err := conn.Do("XGROUP", "CREATE", streamName, groupName, i.startID, "MKSTREAM")
	if err != nil {
		log.Printf("error on XGROUP CREATE: %v\n", err)
	}
}

func parseReply(reply interface{}) (ret []RedisStreamEvent) {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("unexpected error when parsing reply: %v\n", err)
			debug.PrintStack()
		}
	}()

	streamAndEventss := reply.([]interface{})
	for _, se := range streamAndEventss {
		streamAndEvents := se.([]interface{})
		events := streamAndEvents[1].([]interface{})
		for _, e := range events {
			event := e.([]interface{})
			eventID := string(event[0].([]byte))
			var eventData []interface{}
			if event[1] != nil {
				eventData = event[1].([]interface{})
			}
			ret = append(ret, RedisStreamEvent{
				ID:          eventID,
				DeviceEvent: parseEventData(eventData),
			})
		}
	}
	return ret
}

func parseEventData(eventData []interface{}) *DeviceEvent {
	if eventData == nil {
		return nil
	}
	ret := &DeviceEvent{}
	for i := 0; i+1 < len(eventData); i += 2 {
		k := string(eventData[i].([]byte))
		v := eventData[i+1].([]byte)
		switch k {
		case "object":
			if err := json.Unmarshal(v, &ret.Object); err != nil {
				log.Printf("unexpected error when unmarshaling object: %v\n", err)
			}
		case "state":
			if err := json.Unmarshal(v, &ret.State); err != nil {
				log.Printf("unexpected error when unmarshaling state: %v\n", err)
			}
		default:
			log.Printf("unexpected key when parsing event data: %v\n", k)
		}
	}
	return ret
}

// String is used in logs.
func (e *DeviceEvent) String() string {
	return fmt.Sprintf("device=%s timestamp=%s", e.Object.UID, e.State.Timestamp)
}
