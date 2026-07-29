package inbox

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/tus/tusd/v2/pkg/handler"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/store"
)

func (in *Inbox) Cancel(ctx context.Context, transferID string) error {
	if _, ok := in.tracked(transferID); !ok {
		return fmt.Errorf("cancel upload %s: %w", transferID, ErrNotActive)
	}
	if err := in.terminate(ctx, transferID); err != nil {
		return fmt.Errorf("cancel upload %s: %w", transferID, err)
	}
	if err := in.markCanceled(ctx, transferID); err != nil {
		return fmt.Errorf("cancel upload %s: %w", transferID, err)
	}
	return nil
}

func (in *Inbox) CancelDevice(ctx context.Context, deviceID string) error {
	var errs []error
	for _, id := range in.trackedOf(deviceID) {
		if err := in.Cancel(ctx, id); err != nil && !errors.Is(err, ErrNotActive) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (in *Inbox) lock(ctx context.Context, transferID string) (handler.Lock, error) {
	lock, err := in.locker.NewLock(transferID)
	if err != nil {
		return nil, err
	}
	lockCtx, cancel := context.WithTimeout(ctx, acquireLockTimeout)
	defer cancel()
	if err := lock.Lock(lockCtx, func() {}); err != nil {
		return nil, err
	}
	return lock, nil
}

func (in *Inbox) terminate(ctx context.Context, transferID string) error {
	lock, err := in.lock(ctx, transferID)
	if err != nil {
		return err
	}
	return errors.Join(in.removeFiles(ctx, transferID), lock.Unlock())
}

func (in *Inbox) removeFiles(ctx context.Context, transferID string) error {
	u, err := in.files.GetUpload(ctx, transferID)
	if errors.Is(err, handler.ErrNotFound) {
		return in.removeLeftovers(transferID)
	}
	if err != nil {
		return err
	}
	return in.files.AsTerminatableUpload(u).Terminate(ctx)
}

func (in *Inbox) removeLeftovers(transferID string) error {
	var errs []error
	for _, name := range []string{transferID, transferID + infoSuffix} {
		if err := in.root.Remove(in.incomingPath(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (in *Inbox) markCanceled(ctx context.Context, transferID string) error {
	return in.markFinished(ctx, transferID, store.TransferCanceled, "")
}

func (in *Inbox) markExpired(ctx context.Context, transferID string) error {
	return in.markFinished(ctx, transferID, store.TransferFailed, api.CodeExpired)
}

func (in *Inbox) markFinished(ctx context.Context, transferID string, status store.TransferStatus, code api.ErrorCode) error {
	ctx = context.WithoutCancel(ctx)
	t, ok := in.untrack(transferID)
	if !ok {
		stored, err := in.store.Transfer(ctx, transferID)
		if errors.Is(err, store.ErrNotFound) || (err == nil && stored.Status != store.TransferActive) {
			return ErrNotActive
		}
		if err != nil {
			return err
		}
		t = stored
	}
	t.Status = status
	t.Error = string(code)
	t.UpdatedAt = time.Now()
	if err := in.store.UpdateTransfer(ctx, t); err != nil {
		return err
	}
	in.publish(t)
	in.log.Info("upload ended", "transfer", transferID, "device", t.DeviceID, "status", string(status), "bytes", t.Done)
	return nil
}
