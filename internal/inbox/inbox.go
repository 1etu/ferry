package inbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tus/tusd/v2/pkg/filestore"
	"github.com/tus/tusd/v2/pkg/handler"
	"github.com/tus/tusd/v2/pkg/memorylocker"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/seal"
	"github.com/1etu/ferry/internal/store"
)

var (
	ErrNotActive       = errors.New("upload not active")
	ErrShutdown  error = handler.ErrServerShutdown
)

const (
	basePath                = "/api/uploads/"
	defaultProgressInterval = 500 * time.Millisecond
	publishInterval         = 250 * time.Millisecond
	acquireLockTimeout      = 20 * time.Second
	incomingDirPerm         = 0o750
	infoSuffix              = ".info"
	jsonContentType         = "application/json; charset=utf-8"
	tusContentType          = "application/offset+octet-stream"
	retryAfterSeconds       = "1"
	writeBufferBytes        = seal.FrameSize
)

type Config struct {
	ReceivedDir        string
	IncomingDir        string
	MaxUploadBytes     int64
	ReserveBytes       int64
	MaxActivePerDevice int
	ProgressInterval   time.Duration
	DeviceID           func(ctx context.Context) (string, bool)
	Session            func(ctx context.Context) (seal.Session, bool)
	FreeSpace          func(dir string) (uint64, error)
}

type Inbox struct {
	cfg      Config
	store    *store.Store
	hub      *events.Hub
	log      *slog.Logger
	root     *os.Root
	incoming string
	files    *uploadStore
	locker   *memorylocker.MemoryLocker
	tus      *handler.Handler
	buffers  bufferPool
	rings    bufferPool

	mu      sync.Mutex
	uploads map[string]*upload
	writes  map[string]int

	stopDrain chan struct{}
	drained   chan struct{}
}

type upload struct {
	transfer    store.Transfer
	info        *handler.FileInfo
	done        int64
	publishedAt time.Time
	isAnnounced bool
}

func New(cfg Config, st *store.Store, hub *events.Hub, log *slog.Logger) (*Inbox, error) {
	incoming, err := filepath.Rel(cfg.ReceivedDir, cfg.IncomingDir)
	if err != nil || incoming == "." || !filepath.IsLocal(incoming) {
		return nil, fmt.Errorf("incoming dir %s is not inside received dir %s", cfg.IncomingDir, cfg.ReceivedDir)
	}
	if err := os.MkdirAll(cfg.IncomingDir, incomingDirPerm); err != nil {
		return nil, fmt.Errorf("create incoming dir %s: %w", cfg.IncomingDir, err)
	}
	root, err := os.OpenRoot(cfg.ReceivedDir)
	if err != nil {
		return nil, fmt.Errorf("open received dir %s: %w", cfg.ReceivedDir, err)
	}
	if cfg.ProgressInterval <= 0 {
		cfg.ProgressInterval = defaultProgressInterval
	}
	in := &Inbox{
		cfg:       cfg,
		store:     st,
		hub:       hub,
		log:       log,
		root:      root,
		incoming:  incoming,
		locker:    memorylocker.New(),
		buffers:   bufferPool{bytes: writeBufferBytes},
		rings:     bufferPool{bytes: readAheadBytes},
		uploads:   make(map[string]*upload),
		writes:    make(map[string]int),
		stopDrain: make(chan struct{}),
		drained:   make(chan struct{}),
	}
	in.files = &uploadStore{disk: filestore.New(cfg.IncomingDir), in: in}
	composer := handler.NewStoreComposer()
	composer.UseCore(in.files)
	composer.UseTerminater(in.files)
	composer.UseLengthDeferrer(in.files)
	composer.UseLocker(in.locker)
	in.tus, err = handler.NewHandler(handler.Config{
		StoreComposer:              composer,
		BasePath:                   basePath,
		DisableDownload:            true,
		Cors:                       &handler.CorsConfig{Disable: true},
		NotifyUploadProgress:       true,
		UploadProgressInterval:     cfg.ProgressInterval,
		PreUploadCreateCallback:    in.preCreate,
		PreFinishResponseCallback:  in.preFinish,
		PreUploadTerminateCallback: in.preTerminate,
		AcquireLockTimeout:         acquireLockTimeout,
		Logger:                     tusLogger(log),
	})
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create tus handler: %w", err), root.Close())
	}
	go in.drainProgress()
	return in, nil
}

func (in *Inbox) Close(ctx context.Context) error {
	close(in.stopDrain)
	<-in.drained
	now := time.Now()
	in.mu.Lock()
	snapshot := make([]store.Transfer, 0, len(in.uploads))
	for _, u := range in.uploads {
		t := u.transfer
		t.Done = u.done
		t.UpdatedAt = now
		snapshot = append(snapshot, t)
	}
	in.mu.Unlock()
	errs := make([]error, 0, len(snapshot)+1)
	for i := range snapshot {
		if err := in.store.UpdateTransfer(ctx, snapshot[i]); err != nil {
			errs = append(errs, fmt.Errorf("save progress of upload %s: %w", snapshot[i].ID, err))
		}
	}
	return errors.Join(append(errs, in.root.Close())...)
}

func (in *Inbox) publish(t store.Transfer) {
	in.hub.Publish(events.Event{Kind: events.KindTransfer, DeviceID: t.DeviceID, Payload: api.TransferFrom(t)})
}

func (in *Inbox) incomingPath(name string) string {
	return filepath.Join(in.incoming, name)
}

func (in *Inbox) hasSpaceFor(bytes int64) (bool, error) {
	free, err := in.cfg.FreeSpace(in.cfg.ReceivedDir)
	if err != nil {
		return false, fmt.Errorf("free space of %s: %w", in.cfg.ReceivedDir, err)
	}
	return fitsFreeSpace(free, bytes, in.outstanding(), in.cfg.ReserveBytes), nil
}

func fitsFreeSpace(free uint64, size, outstanding, reserve int64) bool {
	needed := size + outstanding + reserve
	if needed < 0 {
		return false
	}
	return free >= uint64(needed)
}
