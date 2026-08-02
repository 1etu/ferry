package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/store"
)

var ErrNotRegularFile = errors.New("not a regular file")

const progressInterval = 250 * time.Millisecond

type Outbox struct {
	store            *store.Store
	hub              *events.Hub
	deviceID         func(ctx context.Context) (string, bool)
	log              *slog.Logger
	progressInterval time.Duration

	transferMu sync.Mutex

	mu   sync.Mutex
	live map[string]*liveTransfer
}

type liveTransfer struct {
	transfer    store.Transfer
	covered     coverage
	publishedAt time.Time
	requests    int
	isSettled   bool
}

func New(st *store.Store, hub *events.Hub, deviceID func(ctx context.Context) (string, bool), log *slog.Logger) *Outbox {
	return &Outbox{
		store:            st,
		hub:              hub,
		deviceID:         deviceID,
		log:              log,
		progressInterval: progressInterval,
		live:             make(map[string]*liveTransfer),
	}
}

func (o *Outbox) Offer(ctx context.Context, paths []string) ([]store.File, error) {
	now := time.Now()
	offered := make([]store.File, 0, len(paths))
	for _, path := range paths {
		f, err := regularFile(path)
		if err != nil {
			return nil, err
		}
		f.ID = ulid.Make().String()
		f.CreatedAt = now
		offered = append(offered, f)
	}
	for i, f := range offered {
		if err := o.store.InsertFile(ctx, f); err != nil {
			return nil, fmt.Errorf("offer %s: %w", f.Path, err)
		}
		offered[i] = storedFile(f)
	}
	for _, f := range offered {
		o.publishFile(api.FileAdded, f)
		o.log.Info("file offered", "file", f.ID, "path", f.Path, "bytes", f.Size)
	}
	return offered, nil
}

func regularFile(path string) (store.File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return store.File{}, fmt.Errorf("offer %s: %w: %w", path, ErrNotRegularFile, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return store.File{}, fmt.Errorf("offer %s: %w: %w", abs, ErrNotRegularFile, err)
	}
	if !info.Mode().IsRegular() {
		return store.File{}, fmt.Errorf("offer %s: %w", abs, ErrNotRegularFile)
	}
	return store.File{Path: abs, Name: filepath.Base(abs), Size: info.Size(), ModTime: info.ModTime()}, nil
}

func storedFile(f store.File) store.File {
	f.ModTime = time.UnixMilli(f.ModTime.UnixMilli()).UTC()
	f.CreatedAt = time.UnixMilli(f.CreatedAt.UnixMilli()).UTC()
	return f
}

func (o *Outbox) Remove(ctx context.Context, id string) error {
	f, err := o.store.File(ctx, id)
	if err != nil {
		return fmt.Errorf("remove offered file: %w", err)
	}
	if err := o.store.DeleteFile(ctx, id); err != nil {
		return fmt.Errorf("remove offered file: %w", err)
	}
	o.publishFile(api.FileRemoved, f)
	return nil
}

func (o *Outbox) Sweep(ctx context.Context, olderThan time.Duration) error {
	swept, err := o.store.DeleteFilesBefore(ctx, time.Now().Add(-olderThan))
	if err != nil {
		return fmt.Errorf("sweep offered files: %w", err)
	}
	for _, f := range swept {
		o.publishFile(api.FileRemoved, f)
	}
	if len(swept) > 0 {
		o.log.Info("offered files swept", "count", len(swept))
	}
	return nil
}

func (o *Outbox) Progress(transferID string) (done int64, ok bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	lt, ok := o.live[transferID]
	if !ok {
		return 0, false
	}
	return lt.covered.prefix(), true
}

func (o *Outbox) track(t store.Transfer) {
	o.mu.Lock()
	defer o.mu.Unlock()
	lt, ok := o.live[t.ID]
	if !ok {
		lt = &liveTransfer{transfer: t, covered: coveredPrefix(t.Done), publishedAt: time.Now()}
		o.live[t.ID] = lt
	}
	lt.requests++
}

func (o *Outbox) advance(transferID string, start, end int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	lt := o.live[transferID]
	lt.covered = lt.covered.add(start, end)
	now := time.Now()
	if lt.isSettled || now.Sub(lt.publishedAt) < o.progressInterval {
		return
	}
	lt.publishedAt = now
	snapshot := lt.transfer
	snapshot.Done = lt.covered.prefix()
	snapshot.UpdatedAt = now
	o.publishTransfer(snapshot)
}

func (o *Outbox) untrack(transferID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	lt := o.live[transferID]
	lt.requests--
	if lt.requests == 0 {
		delete(o.live, transferID)
	}
}

func (o *Outbox) publishStored(t store.Transfer) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if lt, ok := o.live[t.ID]; ok {
		if lt.isSettled {
			return
		}
		lt.isSettled = t.Status != store.TransferActive
		if !lt.isSettled {
			t.Done = max(t.Done, lt.covered.prefix())
		}
	}
	o.publishTransfer(t)
}

func (o *Outbox) publishFile(action string, f store.File) {
	o.hub.Publish(events.Event{Kind: events.KindFile, Payload: api.FileChange{Action: action, File: api.FileFrom(f)}})
}

func (o *Outbox) publishTransfer(t store.Transfer) {
	o.hub.Publish(events.Event{Kind: events.KindTransfer, DeviceID: t.DeviceID, Payload: api.TransferFrom(t)})
}
