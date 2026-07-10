package store

import (
	"path/filepath"
	"testing"
	"time"
)

var epoch = time.UnixMilli(1_790_000_000_000).UTC()

func at(minutes int) time.Time {
	return epoch.Add(time.Duration(minutes) * time.Minute)
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	return openStoreAt(t, filepath.Join(t.TempDir(), "ferry.db"))
}

func openStoreAt(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return s
}

func device(id string) Device {
	return Device{
		ID:        id,
		Name:      "iPhone " + id,
		TokenHash: []byte("hash-" + id),
		Status:    DevicePending,
		CreatedAt: at(0),
	}
}

func transfer(id, deviceID string, status TransferStatus) Transfer {
	return Transfer{
		ID:        id,
		DeviceID:  deviceID,
		Direction: DirectionIn,
		Name:      id + ".jpg",
		Size:      1000,
		Status:    status,
		CreatedAt: at(1),
		UpdatedAt: at(1),
	}
}

func file(id string, createdAt time.Time) File {
	return File{
		ID:        id,
		Path:      `C:\Users\owner\` + id + ".mov",
		Name:      id + ".mov",
		Size:      2048,
		ModTime:   at(-60),
		CreatedAt: createdAt,
	}
}

func mustInsertDevices(t *testing.T, s *Store, devices ...Device) {
	t.Helper()
	for i := range devices {
		if err := s.InsertDevice(t.Context(), devices[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func mustInsertTransfers(t *testing.T, s *Store, transfers ...Transfer) {
	t.Helper()
	for i := range transfers {
		if err := s.InsertTransfer(t.Context(), transfers[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func mustInsertFiles(t *testing.T, s *Store, files ...File) {
	t.Helper()
	for _, f := range files {
		if err := s.InsertFile(t.Context(), f); err != nil {
			t.Fatal(err)
		}
	}
}

func transferIDs(transfers []Transfer) []string {
	ids := make([]string, 0, len(transfers))
	for i := range transfers {
		ids = append(ids, transfers[i].ID)
	}
	return ids
}

func fileIDs(files []File) []string {
	ids := make([]string, 0, len(files))
	for _, f := range files {
		ids = append(ids, f.ID)
	}
	return ids
}
