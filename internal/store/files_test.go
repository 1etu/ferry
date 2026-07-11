package store

import (
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestFileRoundTrip(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	want := file("f1", at(0))
	want.Name = "Ünïcode clip.mov"
	mustInsertFiles(t, s, want)
	got, err := s.File(t.Context(), "f1")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("File (-want +got):\n%s", diff)
	}
}

func TestFilesNewestFirst(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	empty, err := s.Files(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("Files on empty store = %#v, want empty non-nil slice", empty)
	}
	mustInsertFiles(t, s, file("01B", at(0)), file("01A", at(0)), file("01C", at(0)))
	files, err := s.Files(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"01C", "01B", "01A"}, fileIDs(files)); diff != "" {
		t.Fatalf("file order (-want +got):\n%s", diff)
	}
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
	if err := s.UpdateFile(t.Context(), updated); err != nil {
		t.Fatal(err)
	}
	got, err := s.File(t.Context(), "f1")
	if err != nil {
		t.Fatal(err)
	}
	want := updated
	want.CreatedAt = original.CreatedAt
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("File (-want +got):\n%s", diff)
	}
}

func TestDeleteFile(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	mustInsertFiles(t, s, file("f1", at(0)), file("f2", at(0)))
	if err := s.DeleteFile(t.Context(), "f1"); err != nil {
		t.Fatal(err)
	}
	files, err := s.Files(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{"f2"}, fileIDs(files)); diff != "" {
		t.Fatalf("remaining files (-want +got):\n%s", diff)
	}
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
			if err != nil {
				t.Fatal(err)
			}
			removedIDs := fileIDs(removed)
			slices.Sort(removedIDs)
			if diff := cmp.Diff(tt.wantRemoved, removedIDs); diff != "" {
				t.Fatalf("removed (-want +got):\n%s", diff)
			}
			for _, f := range removed {
				if diff := cmp.Diff(file(f.ID, f.CreatedAt), f); diff != "" {
					t.Fatalf("removed file %s has wrong fields (-want +got):\n%s", f.ID, diff)
				}
			}
			kept, err := s.Files(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.wantKept, fileIDs(kept)); diff != "" {
				t.Fatalf("kept (-want +got):\n%s", diff)
			}
		})
	}
}
