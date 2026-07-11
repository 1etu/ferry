package store

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestTransferRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		transfer Transfer
	}{
		{"incoming transfer keeps null strings empty", transfer("t1", "d1", TransferActive)},
		{"outgoing transfer keeps every field", Transfer{
			ID:        "t2",
			DeviceID:  "d1",
			Direction: DirectionOut,
			Name:      "Résumé.pdf",
			Size:      5 << 30,
			Done:      3 << 30,
			Status:    TransferFailed,
			Error:     "file_missing",
			Path:      `D:\Videos\Résumé.pdf`,
			FileID:    "f1",
			CreatedAt: at(1),
			UpdatedAt: at(9),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := openTestStore(t)
			mustInsertDevices(t, s, device("d1"))
			mustInsertTransfers(t, s, tt.transfer)
			got, err := s.Transfer(t.Context(), tt.transfer.ID)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.transfer, got); diff != "" {
				t.Fatalf("Transfer (-want +got):\n%s", diff)
			}
		})
	}
}

func TestInsertTransferRequiresKnownDevice(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	if err := s.InsertTransfer(t.Context(), transfer("t1", "ghost", TransferActive)); err == nil {
		t.Fatal("inserted a transfer for a device that does not exist")
	}
}

func TestActiveTransfersByDirection(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	mustInsertDevices(t, s, device("d1"), device("d2"))
	outgoing := transfer("01C", "d2", TransferActive)
	outgoing.Direction = DirectionOut
	mustInsertTransfers(t, s,
		transfer("01A", "d1", TransferActive),
		transfer("01B", "d1", TransferDone),
		outgoing,
		transfer("01D", "d2", TransferActive),
		transfer("01E", "d2", TransferCanceled),
	)
	tests := []struct {
		direction Direction
		want      []string
	}{
		{DirectionIn, []string{"01D", "01A"}},
		{DirectionOut, []string{"01C"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.direction), func(t *testing.T) {
			t.Parallel()
			got, err := s.ActiveTransfers(t.Context(), tt.direction)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, transferIDs(got)); diff != "" {
				t.Fatalf("active transfer ids (-want +got):\n%s", diff)
			}
		})
	}
}

func TestActiveTransferFindsTheActiveDownloadOfAFile(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	mustInsertDevices(t, s, device("d1"), device("d2"))
	download := func(id, deviceID, fileID string, status TransferStatus) Transfer {
		tr := transfer(id, deviceID, status)
		tr.Direction = DirectionOut
		tr.FileID = fileID
		return tr
	}
	mustInsertTransfers(t, s,
		download("01A", "d1", "f1", TransferDone),
		download("01B", "d1", "f1", TransferActive),
		download("01C", "d2", "f1", TransferActive),
		download("01D", "d1", "f2", TransferFailed),
	)
	tests := []struct {
		name     string
		deviceID string
		fileID   string
		want     string
	}{
		{"active row wins over finished rows", "d1", "f1", "01B"},
		{"other device has its own row", "d2", "f1", "01C"},
		{"finished row only is not found", "d1", "f2", ""},
		{"file never served to device is not found", "d2", "f2", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := s.ActiveTransfer(t.Context(), tt.deviceID, tt.fileID)
			if tt.want == "" {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("error = %v, want ErrNotFound", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != tt.want {
				t.Fatalf("id = %q, want %q", got.ID, tt.want)
			}
		})
	}
}

func TestUpdateTransferChangesMutableFields(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	mustInsertDevices(t, s, device("d1"))
	original := transfer("t1", "d1", TransferActive)
	mustInsertTransfers(t, s, original)
	updated := original
	updated.Name = "Photo (2).jpg"
	updated.Done = 1000
	updated.Status = TransferDone
	updated.Path = `C:\Users\owner\Downloads\Ferry\Photo (2).jpg`
	updated.UpdatedAt = at(7)
	if err := s.UpdateTransfer(t.Context(), updated); err != nil {
		t.Fatal(err)
	}
	got, err := s.Transfer(t.Context(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(updated, got); diff != "" {
		t.Fatalf("Transfer (-want +got):\n%s", diff)
	}

	cleared := updated
	cleared.Path = ""
	cleared.Error = ""
	if err := s.UpdateTransfer(t.Context(), cleared); err != nil {
		t.Fatal(err)
	}
	var nullPaths int
	err = s.db.QueryRowContext(t.Context(), "SELECT count(*) FROM transfers WHERE path IS NULL").Scan(&nullPaths)
	if err != nil {
		t.Fatal(err)
	}
	if nullPaths != 1 {
		t.Fatalf("transfers with null path = %d, want 1", nullPaths)
	}
}

func TestUpdateTransferKeepsCreationFields(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	mustInsertDevices(t, s, device("d1"), device("d2"))
	original := transfer("t1", "d1", TransferActive)
	mustInsertTransfers(t, s, original)
	changed := original
	changed.DeviceID = "d2"
	changed.Direction = DirectionOut
	changed.CreatedAt = at(99)
	if err := s.UpdateTransfer(t.Context(), changed); err != nil {
		t.Fatal(err)
	}
	got, err := s.Transfer(t.Context(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(original, got); diff != "" {
		t.Fatalf("Transfer (-want +got):\n%s", diff)
	}
}

func TestDeleteTransfer(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	mustInsertDevices(t, s, device("d1"))
	mustInsertTransfers(t, s, transfer("t1", "d1", TransferDone), transfer("t2", "d1", TransferDone))
	if err := s.DeleteTransfer(t.Context(), "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transfer(t.Context(), "t1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted transfer: error = %v, want ErrNotFound", err)
	}
	if _, err := s.Transfer(t.Context(), "t2"); err != nil {
		t.Fatalf("other transfer: %v", err)
	}
}

func TestDeleteFinishedTransfersKeepsActiveRows(t *testing.T) {
	t.Parallel()
	seed := []Transfer{
		transfer("01A", "d1", TransferActive),
		transfer("01B", "d1", TransferDone),
		transfer("01C", "d1", TransferFailed),
		transfer("01D", "d1", TransferCanceled),
		transfer("01E", "d2", TransferActive),
		transfer("01F", "d2", TransferDone),
	}
	tests := []struct {
		name     string
		deviceID string
		want     []string
	}{
		{"empty device id clears every device", "", []string{"01E", "01A"}},
		{"device id clears only that device", "d1", []string{"01F", "01E", "01A"}},
		{"unknown device clears nothing", "d9", []string{"01F", "01E", "01D", "01C", "01B", "01A"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := openTestStore(t)
			mustInsertDevices(t, s, device("d1"), device("d2"))
			mustInsertTransfers(t, s, seed...)
			if err := s.DeleteFinishedTransfers(t.Context(), tt.deviceID); err != nil {
				t.Fatal(err)
			}
			got, err := s.Transfers(t.Context(), "", 100)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, transferIDs(got)); diff != "" {
				t.Fatalf("remaining transfer ids (-want +got):\n%s", diff)
			}
		})
	}
}
