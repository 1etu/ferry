package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/1etu/ferry/internal/update"
)

var errQuitting = errors.New("ferry is quitting")

func (m *machine) applyUpdate(ctx context.Context) error {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	if ctx.Err() != nil {
		return fmt.Errorf("apply update: %w", errQuitting)
	}
	if m.updater.Status().State != update.StateReady {
		return update.ErrNotReady
	}
	if err := m.stack.stop(ctx); err != nil {
		m.log.Error("transfer stack failed before the update", "err", err)
	}
	err := m.updater.Apply(ctx, m.healthURL())
	if err == nil {
		m.log.Info("new version answering, quitting")
		m.quit()
		return nil
	}
	if startErr := m.stack.start(ctx); startErr != nil && !errors.Is(startErr, errStackClosed) {
		m.stack.fail(fmt.Errorf("restart after a failed update: %w", startErr))
	}
	return fmt.Errorf("apply update: %w", err)
}

func (m *machine) removeOldExe(ctx context.Context) {
	if err := m.updater.RemoveOld(ctx); err != nil && ctx.Err() == nil {
		m.log.Warn("previous version not removed", "err", err)
	}
}
