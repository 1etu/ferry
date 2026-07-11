package store

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestTransfersFiltersByDeviceNewestFirst(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	mustInsertDevices(t, s, device("d1"), device("d2"))
	mustInsertTransfers(t, s,
		transfer("01A", "d1", TransferDone),
		transfer("01D", "d2", TransferActive),
		transfer("01B", "d2", TransferDone),
		transfer("01C", "d1", TransferActive),
	)
	tests := []struct {
		name     string
		deviceID string
		limit    int
		want     []string
	}{
		{"empty device id lists every device", "", 10, []string{"01D", "01C", "01B", "01A"}},
		{"empty device id honors the limit", "", 2, []string{"01D", "01C"}},
		{"device id lists only that device", "d1", 10, []string{"01C", "01A"}},
		{"device id honors the limit", "d2", 1, []string{"01D"}},
		{"device without transfers lists nothing", "d3", 10, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := s.Transfers(t.Context(), tt.deviceID, tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil {
				t.Fatal("Transfers returned nil, want a non-nil slice")
			}
			if diff := cmp.Diff(tt.want, transferIDs(got)); diff != "" {
				t.Fatalf("transfer ids (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTransfersOfDeviceWalkTheDeviceIndexWithoutSorting(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	rows, err := s.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+transfersOfDeviceQuery, "d1", 10)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "USING INDEX transfers_by_device") || strings.Contains(joined, "TEMP B-TREE") {
		t.Fatalf("query plan:\n%s", joined)
	}
}
