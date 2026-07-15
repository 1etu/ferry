package events

import (
	"log/slog"
	"slices"
	"sync"
	"time"
)

type Kind string

const (
	KindTransfer Kind = "transfer"
	KindFile     Kind = "file"
	KindDevice   Kind = "device"
	KindPairing  Kind = "pairing"
	KindUpdate   Kind = "update"
	KindNetwork  Kind = "network"
	KindReset    Kind = "reset"
)

type Event struct {
	Kind      Kind
	DeviceID  string
	OwnerOnly bool
	Payload   any
}

type Scope struct {
	Owner      bool
	DeviceID   string
	IsApproved bool
	Seal       func(plain string) (sealed string, ok bool)
}

type Sealable interface {
	WithSealedNames(seal func(string) (string, bool)) (payload any, ok bool)
}

func (s Scope) accepts(e Event) bool {
	if s.Owner {
		return true
	}
	if e.DeviceID != "" {
		return e.DeviceID == s.DeviceID
	}
	return !e.OwnerOnly && s.IsApproved
}

const (
	subscriberBuffer    = 64
	maxStreamsPerDevice = 4
)

type subscriber struct {
	scope  Scope
	events chan Event
}

type Hub struct {
	log       *slog.Logger
	keepAlive time.Duration

	mu          sync.Mutex
	subscribers []*subscriber
	isClosed    bool
}

func NewHub(log *slog.Logger) *Hub {
	return &Hub{log: log, keepAlive: keepAliveInterval}
}

func (h *Hub) Publish(e Event) {
	var overflowed []*subscriber
	h.mu.Lock()
	for _, s := range h.subscribers {
		if !s.scope.accepts(e) {
			continue
		}
		select {
		case s.events <- e:
		default:
			overflowed = append(overflowed, s)
		}
	}
	for _, s := range overflowed {
		h.removeLocked(s)
	}
	h.mu.Unlock()

	for _, s := range overflowed {
		h.log.Warn("event stream dropped, buffer full", "device", s.scope.DeviceID, "owner", s.scope.Owner)
	}
}

func (h *Hub) Subscribe(scope Scope) (<-chan Event, func()) {
	s := &subscriber{scope: scope, events: make(chan Event, subscriberBuffer)}

	h.mu.Lock()
	if h.isClosed {
		h.mu.Unlock()
		close(s.events)
		return s.events, func() {}
	}
	evicted := h.evictOldestOverCapLocked(scope)
	h.subscribers = append(h.subscribers, s)
	h.mu.Unlock()

	if evicted != nil {
		h.log.Warn("event stream dropped, device stream limit reached", "device", scope.DeviceID)
	}
	return s.events, func() { h.unsubscribe(s) }
}

func (h *Hub) Disconnect(deviceID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.deviceStreamsLocked(deviceID) {
		h.removeLocked(s)
	}
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.isClosed = true
	for _, s := range h.subscribers {
		close(s.events)
	}
	h.subscribers = nil
}

func (h *Hub) unsubscribe(s *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeLocked(s)
}

func (h *Hub) deviceStreamsLocked(deviceID string) []*subscriber {
	var streams []*subscriber
	for _, s := range h.subscribers {
		if !s.scope.Owner && s.scope.DeviceID == deviceID {
			streams = append(streams, s)
		}
	}
	return streams
}

func (h *Hub) evictOldestOverCapLocked(scope Scope) *subscriber {
	if scope.Owner {
		return nil
	}
	streams := h.deviceStreamsLocked(scope.DeviceID)
	if len(streams) < maxStreamsPerDevice {
		return nil
	}
	h.removeLocked(streams[0])
	return streams[0]
}

func (h *Hub) removeLocked(target *subscriber) {
	for i, s := range h.subscribers {
		if s == target {
			h.subscribers = slices.Delete(h.subscribers, i, i+1)
			close(s.events)
			return
		}
	}
}
