package inbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tus/tusd/v2/pkg/handler"

	"github.com/1etu/ferry/internal/store"
)

func (in *Inbox) finishIfComplete(ctx context.Context, transferID string) (isLive bool, err error) {
	lock, err := in.lock(ctx, transferID)
	if err != nil {
		return false, err
	}
	t, ok := in.tracked(transferID)
	if !ok {
		return false, lock.Unlock()
	}
	info, isLive, err := in.liveInfo(transferID)
	switch {
	case errors.Is(err, handler.ErrNotFound):
		return true, lock.Unlock()
	case err != nil:
		return false, errors.Join(err, lock.Unlock())
	case !isLive:
		return false, lock.Unlock()
	case info.Offset < t.Size:
		return true, lock.Unlock()
	}
	t.Done = info.Offset
	return false, errors.Join(in.complete(ctx, t), lock.Unlock())
}

func (in *Inbox) complete(ctx context.Context, t store.Transfer) error {
	ctx = context.WithoutCancel(ctx)
	final, err := in.claim(ctx, t)
	if err != nil {
		return err
	}
	if err := in.moveIntoPlace(t.ID, final); err != nil {
		return errors.Join(err, in.root.Remove(final))
	}
	in.untrack(t.ID)
	if err := in.root.Remove(in.incomingPath(t.ID + infoSuffix)); err != nil {
		in.log.Warn("upload info file not removed", "transfer", t.ID, "err", err)
	}
	in.markDone(ctx, t, final)
	return nil
}

func (in *Inbox) moveIntoPlace(transferID, final string) error {
	if err := in.syncData(transferID); err != nil {
		return err
	}
	if err := in.root.Rename(in.incomingPath(transferID), final); err != nil {
		return fmt.Errorf("move upload %s to %s: %w", transferID, final, err)
	}
	return nil
}

func (in *Inbox) syncData(transferID string) error {
	f, err := in.root.OpenFile(in.incomingPath(transferID), os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("sync upload %s: %w", transferID, err)
	}
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return fmt.Errorf("sync upload %s: %w", transferID, err)
	}
	return nil
}

func (in *Inbox) markDone(ctx context.Context, t store.Transfer, final string) {
	t.Name = final
	t.Path = filepath.Join(in.cfg.ReceivedDir, final)
	t.Done = t.Size
	t.Status = store.TransferDone
	t.Error = ""
	t.UpdatedAt = time.Now()
	if err := in.store.UpdateTransfer(ctx, t); err != nil {
		in.log.Error("completed upload not recorded, file is in place", "transfer", t.ID, "path", t.Path, "err", err)
	}
	in.publish(t)
	in.log.Info("upload complete", "transfer", t.ID, "device", t.DeviceID, "path", t.Path, "bytes", t.Size)
}
