package events

import (
	"log/slog"
	"slices"
	"testing"
)

func newTestHub() *Hub {
	return NewHub(slog.New(slog.DiscardHandler))
}

func isClosed(ch <-chan Event) bool {
	select {
	case _, ok := <-ch:
		return !ok
	default:
		return false
	}
}

func subscribe(t *testing.T, h *Hub, scope Scope) <-chan Event {
	t.Helper()
	events, cancel := h.Subscribe(scope)
	t.Cleanup(cancel)
	return events
}

func (h *Hub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subscribers)
}

func TestPublishDeliversByScope(t *testing.T) {
	t.Parallel()
	owner := Scope{Owner: true}
	approved := Scope{DeviceID: "a", IsApproved: true}
	pending := Scope{DeviceID: "a"}
	tests := []struct {
		name  string
		scope Scope
		event Event
		want  bool
	}{
		{"owner receives a device event", owner, Event{Kind: KindDevice, DeviceID: "a"}, true},
		{"owner receives an owner-only event", owner, Event{Kind: KindPairing, OwnerOnly: true}, true},
		{"owner receives a broadcast", owner, Event{Kind: KindFile}, true},
		{"approved device receives its own event", approved, Event{Kind: KindTransfer, DeviceID: "a"}, true},
		{"approved device skips another device's event", approved, Event{Kind: KindTransfer, DeviceID: "b"}, false},
		{"approved device receives a broadcast", approved, Event{Kind: KindFile}, true},
		{"approved device skips an owner-only broadcast", approved, Event{Kind: KindPairing, OwnerOnly: true}, false},
		{"pending device receives its own event", pending, Event{Kind: KindDevice, DeviceID: "a"}, true},
		{"pending device skips a broadcast", pending, Event{Kind: KindFile}, false},
		{"pending device skips a reset", pending, Event{Kind: KindReset}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newTestHub()
			events, cancel := h.Subscribe(tc.scope)
			defer cancel()
			h.Publish(tc.event)
			var got bool
			select {
			case e := <-events:
				got = true
				if e.Kind != tc.event.Kind {
					t.Fatalf("got kind %q, want %q", e.Kind, tc.event.Kind)
				}
			default:
			}
			if got != tc.want {
				t.Fatalf("delivered %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFullSubscriberIsDroppedAndClosed(t *testing.T) {
	t.Parallel()
	h := newTestHub()
	slow, cancelSlow := h.Subscribe(Scope{DeviceID: "a"})
	other, cancelOther := h.Subscribe(Scope{DeviceID: "b"})
	defer cancelOther()

	for i := range subscriberBuffer + 1 {
		h.Publish(Event{Kind: KindTransfer, DeviceID: "a", Payload: i})
	}

	for i := range subscriberBuffer {
		e, ok := <-slow
		if !ok {
			t.Fatalf("channel closed after %d events, want %d buffered", i, subscriberBuffer)
		}
		if e.Payload != i {
			t.Fatalf("got payload %v, want %d", e.Payload, i)
		}
	}
	if _, ok := <-slow; ok {
		t.Fatal("overflowed channel still open")
	}
	cancelSlow()

	if got := h.subscriberCount(); got != 1 {
		t.Fatalf("got %d subscribers, want 1", got)
	}
	h.Publish(Event{Kind: KindTransfer, DeviceID: "b"})
	if e := <-other; e.DeviceID != "b" {
		t.Fatalf("other subscriber got %+v", e)
	}
}

func TestDeviceStreamCapDropsOldest(t *testing.T) {
	t.Parallel()
	h := newTestHub()
	deviceA := make([]<-chan Event, 0, maxStreamsPerDevice)
	for range maxStreamsPerDevice {
		deviceA = append(deviceA, subscribe(t, h, Scope{DeviceID: "a"}))
	}
	unaffected := make([]<-chan Event, 0, 2*maxStreamsPerDevice+1)
	for range maxStreamsPerDevice + 1 {
		unaffected = append(unaffected, subscribe(t, h, Scope{Owner: true}))
	}
	for range maxStreamsPerDevice {
		unaffected = append(unaffected, subscribe(t, h, Scope{DeviceID: "b"}))
	}

	newest := subscribe(t, h, Scope{DeviceID: "a"})
	remaining := slices.Concat(deviceA[1:], []<-chan Event{newest})

	if !isClosed(deviceA[0]) {
		t.Fatal("oldest stream of device a still open")
	}
	for i, events := range remaining {
		if isClosed(events) {
			t.Fatalf("device a stream %d closed, want open", i+1)
		}
	}
	for i, events := range unaffected {
		if isClosed(events) {
			t.Fatalf("unaffected stream %d closed", i)
		}
	}

	h.Publish(Event{Kind: KindTransfer, DeviceID: "a"})
	for i, events := range remaining {
		if e, ok := <-events; !ok || e.DeviceID != "a" {
			t.Fatalf("device a stream %d got %+v, open %v", i+1, e, ok)
		}
	}
}

func TestDisconnectEndsOnlyThatDevicesStreams(t *testing.T) {
	t.Parallel()
	h := newTestHub()
	first := subscribe(t, h, Scope{DeviceID: "a", IsApproved: true})
	second := subscribe(t, h, Scope{DeviceID: "a"})
	other := subscribe(t, h, Scope{DeviceID: "b", IsApproved: true})
	owner := subscribe(t, h, Scope{Owner: true})

	h.Publish(Event{Kind: KindDevice, DeviceID: "a"})
	h.Disconnect("a")

	for i, events := range []<-chan Event{first, second} {
		if e, ok := <-events; !ok || e.Kind != KindDevice {
			t.Fatalf("stream %d lost the event buffered before Disconnect: %+v, open %v", i, e, ok)
		}
		if !isClosed(events) {
			t.Fatalf("stream %d still open after Disconnect", i)
		}
	}
	for name, events := range map[string]<-chan Event{"other device": other, "owner": owner} {
		if isClosed(events) {
			t.Fatalf("%s stream closed by Disconnect of device a", name)
		}
	}
	if got := h.subscriberCount(); got != 2 {
		t.Fatalf("got %d subscribers, want 2", got)
	}
}

func TestCancelStopsDelivery(t *testing.T) {
	t.Parallel()
	h := newTestHub()
	events, cancel := h.Subscribe(Scope{Owner: true})
	cancel()
	if !isClosed(events) {
		t.Fatal("canceled channel still open")
	}
	h.Publish(Event{Kind: KindFile})
	cancel()
	if got := h.subscriberCount(); got != 0 {
		t.Fatalf("got %d subscribers, want 0", got)
	}
}

func TestCloseEndsAllStreams(t *testing.T) {
	t.Parallel()
	h := newTestHub()
	scopes := []Scope{{Owner: true}, {DeviceID: "a"}, {DeviceID: "b"}}
	cancels := make([]func(), 0, len(scopes))
	streams := make([]<-chan Event, 0, len(scopes))
	for _, scope := range scopes {
		events, cancel := h.Subscribe(scope)
		streams = append(streams, events)
		cancels = append(cancels, cancel)
	}

	h.Close()

	for i, events := range streams {
		if !isClosed(events) {
			t.Fatalf("stream %d still open after Close", i)
		}
	}
	for _, cancel := range cancels {
		cancel()
	}
	h.Publish(Event{Kind: KindReset})
	late, cancelLate := h.Subscribe(Scope{Owner: true})
	defer cancelLate()
	if !isClosed(late) {
		t.Fatal("subscription after Close is open")
	}
	if got := h.subscriberCount(); got != 0 {
		t.Fatalf("got %d subscribers, want 0", got)
	}
}

func TestEncodeFrame(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		event Event
		want  string
	}{
		{
			"struct payload as json data",
			Event{Kind: KindTransfer, Payload: struct {
				ID   string `json:"id"`
				Sent int64  `json:"sent"`
			}{"t1", 42}},
			"event: transfer\ndata: {\"id\":\"t1\",\"sent\":42}\n\n",
		},
		{
			"newlines in strings stay escaped on one data line",
			Event{Kind: KindFile, Payload: map[string]string{"name": "a\nb"}},
			"event: file\ndata: {\"name\":\"a\\nb\"}\n\n",
		},
		{
			"nil payload becomes an empty object",
			Event{Kind: KindReset},
			"event: reset\ndata: {}\n\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := encodeFrame(tc.event.Kind, tc.event.Payload)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEncodeFrameRejectsUnencodablePayload(t *testing.T) {
	t.Parallel()
	if _, err := encodeFrame(KindTransfer, make(chan int)); err == nil {
		t.Fatal("got nil error for a channel payload")
	}
}

type namedPayload struct {
	Name string `json:"name"`
}

func (p namedPayload) WithSealedNames(seal func(string) (string, bool)) (any, bool) {
	sealed, ok := seal(p.Name)
	return namedPayload{Name: sealed}, ok
}
