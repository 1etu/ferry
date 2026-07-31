package inbox

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/1etu/ferry/internal/store"
)

func (in *Inbox) Recover(ctx context.Context) error {
	active, err := in.store.ActiveTransfers(ctx, store.DirectionIn)
	if err != nil {
		return fmt.Errorf("recover uploads: %w", err)
	}
	for i := range active {
		if err := in.recover(ctx, active[i]); err != nil {
			return fmt.Errorf("recover uploads: %w", err)
		}
	}
	return nil
}

func (in *Inbox) recover(ctx context.Context, t store.Transfer) error {
	data, dataErr := in.root.Stat(in.incomingPath(t.ID))
	_, infoErr := in.root.Stat(in.incomingPath(t.ID + infoSuffix))
	hasData := dataErr == nil
	isDelivered, err := in.recoverClaim(t, hasData)
	if err != nil {
		return err
	}
	switch {
	case isDelivered:
		if err := in.removeLeftovers(t.ID); err != nil {
			in.log.Warn("upload info file not removed", "transfer", t.ID, "err", err)
		}
		in.markDone(ctx, t, filepath.Base(t.Path))
		return nil
	case !hasData || infoErr != nil:
		if err := in.markExpired(ctx, t.ID); err != nil {
			return err
		}
		in.log.Info("upload lost, transfer failed", "transfer", t.ID, "device", t.DeviceID)
		return nil
	}
	t.Done = data.Size()
	t.UpdatedAt = time.Now()
	if err := in.store.UpdateTransfer(ctx, t); err != nil {
		return err
	}
	in.track(t)
	in.log.Info("upload recovered", "transfer", t.ID, "device", t.DeviceID, "bytes", t.Done)
	if _, err := in.finishIfComplete(ctx, t.ID); err != nil {
		in.log.Error("completed upload not moved into received dir", "transfer", t.ID, "err", err)
	}
	return nil
}

func (in *Inbox) Sweep(ctx context.Context, olderThan time.Duration) error {
	entries, err := fs.ReadDir(in.root.FS(), filepath.ToSlash(in.incoming))
	if err != nil {
		return fmt.Errorf("sweep uploads: %w", err)
	}
	cutoff := time.Now().Add(-olderThan)
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), infoSuffix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if in.isComplete(entry.Name(), info.Size()) {
			if _, err := in.finishIfComplete(ctx, entry.Name()); err != nil {
				errs = append(errs, fmt.Errorf("finish upload %s: %w", entry.Name(), err))
			}
			continue
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		if err := in.expire(ctx, entry.Name()); err != nil {
			errs = append(errs, err)
		}
	}
	for _, entry := range entries {
		id, isInfo := strings.CutSuffix(entry.Name(), infoSuffix)
		if !isInfo || in.hasDataFile(id) {
			continue
		}
		if err := in.root.Remove(in.incomingPath(entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("sweep uploads: %w", err)
	}
	return nil
}

func (in *Inbox) expire(ctx context.Context, transferID string) error {
	if err := in.terminate(ctx, transferID); err != nil {
		return fmt.Errorf("expire upload %s: %w", transferID, err)
	}
	err := in.markExpired(ctx, transferID)
	if errors.Is(err, ErrNotActive) {
		in.log.Info("orphaned upload removed", "transfer", transferID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("expire upload %s: %w", transferID, err)
	}
	return nil
}

func (in *Inbox) hasDataFile(transferID string) bool {
	_, err := in.root.Stat(in.incomingPath(transferID))
	return err == nil
}
