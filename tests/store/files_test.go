package store_test

import (
	"slices"
	"testing"
	"time"

	"github.com/1etu/ferry/tests/kit"
)

func TestFileRoundTrip(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	want := file("f1", at(0))
	want.Name = "Ünïcode clip.mov"
	mustInsertFiles(t, s, want)
	got, err := s.File(t.Context(), "f1")
	kit.NoError(t, err)
	kit.Equal(t, got, want)
}

func TestFilesNewestFirst(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	empty, err := s.Files(t.Context())
	kit.NoError(t, err)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("Files on empty store = %#v, want empty non-nil slice", empty)
	}
	mustInsertFiles(t, s, file("01B", at(0)), file("01A", at(0)), file("01C", at(0)))
	files, err := s.Files(t.Context())
	kit.NoError(t, err)
	kit.Equal(t, fileIDs(files), []string{"01C", "01B", "01A"})
}

func TestUpdateFileRefreshesSizeAndModTime(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	original := file("f1", at(0))
	mustInsertFiles(t, s, original)
	updated := original
	updated.Size = 4096
	updated.ModTime = at(30)
	updated.CreatedAt = at(99)
	kit.NoError(t, s.UpdateFile(t.Context(), updated))
	got, err := s.File(t.Context(), "f1")
	kit.NoError(t, err)
	want := updated
	want.CreatedAt = original.CreatedAt
	kit.Equal(t, got, want)
}

func TestDeleteFile(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	mustInsertFiles(t, s, file("f1", at(0)), file("f2", at(0)))
	kit.NoError(t, s.DeleteFile(t.Context(), "f1"))
	files, err := s.Files(t.Context())
	kit.NoError(t, err)
	kit.Equal(t, fileIDs(files), []string{"f2"})
}

func TestDeleteFilesBeforeReturnsRemovedFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		cutoff      time.Time
		wantRemoved []string
		wantKept    []string
	}{
		{"cutoff before every file removes nothing", at(-1), []string{}, []string{"03", "02", "01"}},
		{"file created exactly at the cutoff is kept", at(10), []string{"01"}, []string{"03", "02"}},
		{"cutoff after every file removes all", at(100), []string{"01", "02", "03"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := openTestStore(t)
			mustInsertFiles(t, s, file("01", at(0)), file("02", at(10)), file("03", at(20)))
			removed, err := s.DeleteFilesBefore(t.Context(), tt.cutoff)
			kit.NoError(t, err)
			removedIDs := fileIDs(removed)
			slices.Sort(removedIDs)
			kit.Equal(t, removedIDs, tt.wantRemoved)
			for _, f := range removed {
				kit.Equal(t, f, file(f.ID, f.CreatedAt))
			}
			kept, err := s.Files(t.Context())
			kit.NoError(t, err)
			kit.Equal(t, fileIDs(kept), tt.wantKept)
		})
	}
}
