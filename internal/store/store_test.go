package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestOpenSetsSchemaVersionTwo(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	if got := userVersion(t, s.db); got != 2 {
		t.Fatalf("user_version = %d, want 2", got)
	}
}

func TestOpenMigratesVersionOne(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ferry.db")
	v1 := strings.NewReplacer("user_version = 2", "user_version = 1", "  secret       BLOB,\n", "").Replace(schema)
	db, err := sql.Open("sqlite", dataSourceName(path))
	if err != nil {
		t.Fatal(err)
	}
	_, execErr := db.ExecContext(t.Context(), v1+
		"INSERT INTO devices (id, name, token_hash, status, created_at) VALUES ('d1', 'iPhone d1', x'01', 'approved', 0);")
	if err := errors.Join(execErr, db.Close()); err != nil {
		t.Fatal(err)
	}

	s := openStoreAt(t, path)
	if got := userVersion(t, s.db); got != 2 {
		t.Fatalf("user_version after migration = %d, want 2", got)
	}
	got, err := s.Device(t.Context(), "d1")
	if err != nil {
		t.Fatalf("device kept across migration: %v", err)
	}
	if got.Secret != nil {
		t.Fatalf("migrated secret = %x, want nil", got.Secret)
	}
}

func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var version int
	if err := db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func TestOpenUsesWALJournal(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	var mode string
	if err := s.db.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}

func TestOpenTwiceOnSameFileSharesData(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ferry.db")
	first := openStoreAt(t, path)
	second := openStoreAt(t, path)
	mustInsertDevices(t, first, device("d1"))
	got, err := second.Device(t.Context(), "d1")
	if err != nil {
		t.Fatalf("second store reads device inserted by first: %v", err)
	}
	if diff := cmp.Diff(device("d1"), got); diff != "" {
		t.Fatalf("device mismatch (-want +got):\n%s", diff)
	}
}

func TestOpenKeepsDataAcrossReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ferry.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	mustInsertDevices(t, s, device("d1"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openStoreAt(t, path)
	if _, err := reopened.Device(t.Context(), "d1"); err != nil {
		t.Fatalf("device after reopen: %v", err)
	}
}

func TestOpenRejectsNewerSchemaVersion(t *testing.T) {
	t.Parallel()
	for _, version := range []int{3, 99} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "ferry.db")
			s, err := Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.ExecContext(t.Context(), fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(t.Context(), path)
			if err == nil {
				reopened.Close()
				t.Fatalf("open succeeded on schema version %d", version)
			}
			want := fmt.Sprintf("unsupported schema version %d", version)
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %v, want it to contain %q", err, want)
			}
		})
	}
}

func TestOpenAcceptsPathsWithURICharacters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		dir  string
	}{
		{"space", "Application Data"},
		{"hash", "Ana#1"},
		{"percent", "100%20done"},
		{"non-ASCII", "Müller"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(t.TempDir(), tt.dir)
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "ferry.db")
			s := openStoreAt(t, path)
			mustInsertDevices(t, s, device("d1"))
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("database file not at the requested path: %v", err)
			}
		})
	}
}

func TestNotFound(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	mustInsertDevices(t, s, device("d1"))
	mustInsertTransfers(t, s, transfer("t1", "d1", TransferDone))
	missingTransfer := transfer("missing", "d1", TransferDone)
	tests := []struct {
		name string
		call func(ctx context.Context) error
	}{
		{"Device", func(ctx context.Context) error { _, err := s.Device(ctx, "missing"); return err }},
		{"DeviceByTokenHash", func(ctx context.Context) error {
			_, err := s.DeviceByTokenHash(ctx, []byte("missing"))
			return err
		}},
		{"SetDeviceStatus", func(ctx context.Context) error {
			return s.SetDeviceStatus(ctx, "missing", DeviceApproved, at(5))
		}},
		{"TouchDevice", func(ctx context.Context) error { return s.TouchDevice(ctx, "missing", at(5)) }},
		{"SetDeviceSecret", func(ctx context.Context) error { return s.SetDeviceSecret(ctx, "missing", []byte("s")) }},
		{"Transfer", func(ctx context.Context) error { _, err := s.Transfer(ctx, "missing"); return err }},
		{"ActiveTransfer with no row", func(ctx context.Context) error {
			_, err := s.ActiveTransfer(ctx, "d1", "f1")
			return err
		}},
		{"UpdateTransfer", func(ctx context.Context) error { return s.UpdateTransfer(ctx, missingTransfer) }},
		{"DeleteTransfer", func(ctx context.Context) error { return s.DeleteTransfer(ctx, "missing") }},
		{"File", func(ctx context.Context) error { _, err := s.File(ctx, "missing"); return err }},
		{"UpdateFile", func(ctx context.Context) error { return s.UpdateFile(ctx, file("missing", at(0))) }},
		{"DeleteFile", func(ctx context.Context) error { return s.DeleteFile(ctx, "missing") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.call(t.Context()); !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestParallelOpenOfExistingDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ferry.db")
	initial, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := initial.Close(); err != nil {
		t.Fatal(err)
	}
	const openers = 4
	stores := make([]*Store, openers)
	errs := make([]error, openers)
	var wg sync.WaitGroup
	for i := range openers {
		wg.Go(func() {
			stores[i], errs[i] = Open(t.Context(), path)
		})
	}
	wg.Wait()
	for i := range openers {
		if stores[i] == nil {
			continue
		}
		if err := stores[i].Close(); err != nil {
			t.Errorf("close opener %d: %v", i, err)
		}
	}
	for i := range openers {
		if errs[i] != nil {
			t.Errorf("opener %d: %v", i, errs[i])
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ferry.db")
	stores := []*Store{openStoreAt(t, path), openStoreAt(t, path)}
	mustInsertDevices(t, stores[0], device("d1"))
	const (
		workers   = 8
		perWorker = 25
	)
	var wg sync.WaitGroup
	errs := make(chan error, workers*perWorker*4)
	for w := range workers {
		s := stores[w%len(stores)]
		wg.Go(func() {
			ctx := t.Context()
			for i := range perWorker {
				tr := transfer(fmt.Sprintf("w%02d-%03d", w, i), "d1", TransferActive)
				if err := s.InsertTransfer(ctx, tr); err != nil {
					errs <- err
					continue
				}
				tr.Done = int64(i)
				tr.Status = TransferDone
				if err := s.UpdateTransfer(ctx, tr); err != nil {
					errs <- err
				}
				if _, err := s.Transfers(ctx, "d1", 10); err != nil {
					errs <- err
				}
				if err := s.TouchDevice(ctx, "d1", at(i)); err != nil {
					errs <- err
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	all, err := stores[1].Transfers(t.Context(), "", workers*perWorker*2)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != workers*perWorker {
		t.Fatalf("transfers = %d, want %d", len(all), workers*perWorker)
	}
	for _, tr := range all {
		if tr.Status != TransferDone {
			t.Fatalf("transfer %s status = %q, want done", tr.ID, tr.Status)
		}
	}
}
