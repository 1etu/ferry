package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/store"
)

const jsonContentType = "application/json; charset=utf-8"

func (o *Outbox) Serve(w http.ResponseWriter, r *http.Request, fileID string) {
	ctx := r.Context()
	f, err := o.store.File(ctx, fileID)
	if errors.Is(err, store.ErrNotFound) {
		o.writeError(w, http.StatusNotFound, api.CodeNotFound, "file not found")
		return
	}
	if err != nil {
		o.log.Error("download failed, offered file not loaded", "file", fileID, "err", err)
		o.writeError(w, http.StatusInternalServerError, api.CodeInternal, "internal error")
		return
	}

	deviceID, isDevice := o.deviceID(ctx)
	isTracked := isDevice && r.Method == http.MethodGet

	content, info, err := openRegular(f.Path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrNotRegularFile) {
		o.log.Warn("download rejected, offered file missing", "file", f.ID, "path", f.Path)
		if isTracked {
			o.failMissing(context.WithoutCancel(ctx), deviceID, f)
		}
		o.writeError(w, http.StatusGone, api.CodeFileMissing, "file missing")
		return
	}
	if err != nil {
		o.log.Error("download failed, offered file not opened", "file", f.ID, "path", f.Path, "err", err)
		o.writeError(w, http.StatusInternalServerError, api.CodeInternal, "internal error")
		return
	}
	defer content.Close()

	f = o.refresh(ctx, f, info)
	setContentHeaders(w.Header(), f, info, r.URL.Query().Get("disposition"))

	if !isTracked {
		http.ServeContent(downloadWriter(w), r, f.Name, info.ModTime(), content)
		return
	}

	t, err := o.activeTransfer(ctx, deviceID, f)
	if err != nil {
		o.log.Error("download failed, transfer not created", "file", f.ID, "device", deviceID, "err", err)
		o.writeError(w, http.StatusInternalServerError, api.CodeInternal, "internal error")
		return
	}
	o.track(t)
	tw := &trackingWriter{ResponseWriter: w, isPooled: hasNoSendfile, cover: func(start, end int64) { o.advance(t.ID, start, end) }}
	defer func() { o.finish(context.WithoutCancel(ctx), t.ID, tw.isCounting) }()
	http.ServeContent(tw, r, f.Name, info.ModTime(), content)
}

func openRegular(path string) (*os.File, os.FileInfo, error) {
	content, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := content.Stat()
	if err != nil {
		return nil, nil, errors.Join(err, content.Close())
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.Join(ErrNotRegularFile, content.Close())
	}
	return content, info, nil
}

func (o *Outbox) refresh(ctx context.Context, f store.File, info os.FileInfo) store.File {
	if f.Size == info.Size() && f.ModTime.UnixMilli() == info.ModTime().UnixMilli() {
		return f
	}
	f.Size = info.Size()
	f.ModTime = info.ModTime()
	if err := o.store.UpdateFile(ctx, f); err != nil {
		o.log.Warn("offered file serving with stale metadata, update failed", "file", f.ID, "err", err)
	}
	return f
}

func (o *Outbox) activeTransfer(ctx context.Context, deviceID string, f store.File) (store.Transfer, error) {
	o.transferMu.Lock()
	defer o.transferMu.Unlock()
	return o.activeTransferLocked(ctx, deviceID, f)
}

func (o *Outbox) activeTransferLocked(ctx context.Context, deviceID string, f store.File) (store.Transfer, error) {
	t, err := o.store.ActiveTransfer(ctx, deviceID, f.ID)
	if err == nil {
		if t.Size == f.Size {
			return t, nil
		}
		t.Size = f.Size
		t.UpdatedAt = time.Now()
		if err := o.store.UpdateTransfer(ctx, t); err != nil {
			return store.Transfer{}, err
		}
		return t, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Transfer{}, err
	}

	now := time.Now()
	t = store.Transfer{
		ID:        ulid.Make().String(),
		DeviceID:  deviceID,
		Direction: store.DirectionOut,
		Name:      f.Name,
		Size:      f.Size,
		Status:    store.TransferActive,
		Path:      f.Path,
		FileID:    f.ID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := o.store.InsertTransfer(ctx, t); err != nil {
		return store.Transfer{}, err
	}
	o.publishTransfer(t)
	return t, nil
}

func (o *Outbox) failMissing(ctx context.Context, deviceID string, f store.File) {
	o.transferMu.Lock()
	defer o.transferMu.Unlock()
	t, err := o.activeTransferLocked(ctx, deviceID, f)
	if err != nil {
		o.log.Error("transfer not marked failed, lookup failed", "file", f.ID, "device", deviceID, "err", err)
		return
	}
	t.Status = store.TransferFailed
	t.Error = string(api.CodeFileMissing)
	t.UpdatedAt = time.Now()
	if err := o.store.UpdateTransfer(ctx, t); err != nil {
		o.log.Error("transfer not marked failed, update failed", "transfer", t.ID, "err", err)
		return
	}
	o.publishStored(t)
}

func (o *Outbox) finish(ctx context.Context, transferID string, isBodyServed bool) {
	defer o.untrack(transferID)
	o.transferMu.Lock()
	defer o.transferMu.Unlock()
	served, _ := o.Progress(transferID)

	t, err := o.store.Transfer(ctx, transferID)
	if err != nil {
		o.log.Error("download progress not saved, transfer not loaded", "transfer", transferID, "err", err)
		return
	}
	if t.Status != store.TransferActive {
		return
	}
	done := max(t.Done, served)
	isComplete := done >= t.Size && (t.Size > 0 || isBodyServed)
	if done == t.Done && !isComplete {
		return
	}
	t.Done = done
	t.UpdatedAt = time.Now()
	if isComplete {
		t.Status = store.TransferDone
	}
	if err := o.store.UpdateTransfer(ctx, t); err != nil {
		o.log.Error("download progress not saved, update failed", "transfer", transferID, "err", err)
		return
	}
	o.publishStored(t)
	if isComplete {
		o.log.Info("download complete", "transfer", t.ID, "device", t.DeviceID, "file", t.FileID, "bytes", t.Size)
	}
}

func (o *Outbox) Cancel(ctx context.Context, transferID string) error {
	o.transferMu.Lock()
	defer o.transferMu.Unlock()
	t, err := o.store.Transfer(ctx, transferID)
	if err != nil {
		return fmt.Errorf("cancel download %s: %w", transferID, err)
	}
	if t.Status != store.TransferActive {
		return nil
	}
	if served, ok := o.Progress(t.ID); ok {
		t.Done = max(t.Done, served)
	}
	t.Status = store.TransferCanceled
	t.UpdatedAt = time.Now()
	if err := o.store.UpdateTransfer(ctx, t); err != nil {
		return fmt.Errorf("cancel download %s: %w", t.ID, err)
	}
	o.publishStored(t)
	o.log.Info("download canceled", "transfer", t.ID, "device", t.DeviceID)
	return nil
}

func (o *Outbox) writeError(w http.ResponseWriter, status int, code api.ErrorCode, message string) {
	w.Header().Set("Content-Type", jsonContentType)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(api.NewError(code, message)); err != nil {
		o.log.Debug("error response not written, client gone", "err", err)
	}
}
