package inbox

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/1etu/ferry/internal/store"
)

const (
	claimFilePerm    = 0o644
	maxClaimAttempts = 10000
)

var errTooManyCollisions = errors.New("too many files with the same name")

func (in *Inbox) claim(ctx context.Context, t store.Transfer) (string, error) {
	for attempt := 1; attempt <= maxClaimAttempts; attempt++ {
		candidate := numberedName(t.Name, attempt)
		if _, err := in.root.Stat(candidate); err == nil {
			continue
		}
		t.Path = filepath.Join(in.cfg.ReceivedDir, candidate)
		t.UpdatedAt = time.Now()
		if err := in.store.UpdateTransfer(ctx, t); err != nil {
			return "", fmt.Errorf("claim %s: %w", candidate, err)
		}
		f, err := in.root.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, claimFilePerm)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("claim %s: %w", candidate, err)
		}
		if err := f.Close(); err != nil {
			return "", fmt.Errorf("claim %s: %w", candidate, err)
		}
		return candidate, nil
	}
	return "", fmt.Errorf("claim %s: %w", t.Name, errTooManyCollisions)
}

func (in *Inbox) recoverClaim(t store.Transfer, hasData bool) (isDelivered bool, err error) {
	if t.Path == "" {
		return false, nil
	}
	claimed := filepath.Base(t.Path)
	info, err := in.root.Stat(claimed)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("recover claim of upload %s: %w", t.ID, err)
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	if !hasData && info.Size() == t.Size {
		return true, nil
	}
	if info.Size() == 0 {
		return false, in.root.Remove(claimed)
	}
	return false, nil
}
