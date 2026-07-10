package store

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestDeviceRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		device Device
	}{
		{"pending device keeps null timestamps zero", device("d1")},
		{"approved device keeps every timestamp", Device{
			ID:         "d2",
			Name:       "Ana’s iPhone",
			TokenHash:  []byte{0, 1, 2, 255},
			Status:     DeviceApproved,
			Secret:     bytes.Repeat([]byte{0xA5}, 32),
			CreatedAt:  at(0),
			ApprovedAt: at(2),
			LastSeenAt: at(3),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := openTestStore(t)
			mustInsertDevices(t, s, tt.device)
			byID, err := s.Device(t.Context(), tt.device.ID)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.device, byID); diff != "" {
				t.Fatalf("Device (-want +got):\n%s", diff)
			}
			byHash, err := s.DeviceByTokenHash(t.Context(), tt.device.TokenHash)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.device, byHash); diff != "" {
				t.Fatalf("DeviceByTokenHash (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNullTimestampsAreStoredAsNull(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	mustInsertDevices(t, s, device("d1"))
	var nulls int
	err := s.db.QueryRowContext(t.Context(),
		"SELECT count(*) FROM devices WHERE approved_at IS NULL AND last_seen_at IS NULL").Scan(&nulls)
	if err != nil {
		t.Fatal(err)
	}
	if nulls != 1 {
		t.Fatalf("rows with null timestamps = %d, want 1", nulls)
	}
}

func TestSetDeviceSecret(t *testing.T) {
	t.Parallel()
	secret := bytes.Repeat([]byte{7}, 32)
	other := bytes.Repeat([]byte{9}, 32)
	tests := []struct {
		name       string
		steps      [][]byte
		wantSecret []byte
	}{
		{"new device has no secret", nil, nil},
		{"secret round trips", [][]byte{secret}, secret},
		{"secret can be replaced", [][]byte{secret, other}, other},
		{"nil clears the secret", [][]byte{secret, nil}, nil},
		{"empty clears the secret", [][]byte{secret, {}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := openTestStore(t)
			mustInsertDevices(t, s, device("d1"))
			for _, step := range tt.steps {
				if err := s.SetDeviceSecret(t.Context(), "d1", step); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.Device(t.Context(), "d1")
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.wantSecret, got.Secret); diff != "" {
				t.Fatalf("secret (-want +got):\n%s", diff)
			}
			var isNull bool
			err = s.db.QueryRowContext(t.Context(), "SELECT secret IS NULL FROM devices WHERE id = 'd1'").Scan(&isNull)
			if err != nil {
				t.Fatal(err)
			}
			if isNull != (tt.wantSecret == nil) {
				t.Fatalf("secret IS NULL = %v, want %v", isNull, tt.wantSecret == nil)
			}
		})
	}
}

func TestInsertDeviceRejectsDuplicateTokenHash(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	mustInsertDevices(t, s, device("d1"))
	duplicate := device("d2")
	duplicate.TokenHash = device("d1").TokenHash
	if err := s.InsertDevice(t.Context(), duplicate); err == nil {
		t.Fatal("inserted a second device with the same token hash")
	}
}

func TestDevicesNewestFirst(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	empty, err := s.Devices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("Devices on empty store = %#v, want empty non-nil slice", empty)
	}
	mustInsertDevices(t, s, device("01A"), device("01C"), device("01B"))
	devices, err := s.Devices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(devices))
	for _, d := range devices {
		ids = append(ids, d.ID)
	}
	if diff := cmp.Diff([]string{"01C", "01B", "01A"}, ids); diff != "" {
		t.Fatalf("device order (-want +got):\n%s", diff)
	}
}

func TestSetDeviceStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		steps          []DeviceStatus
		wantStatus     DeviceStatus
		wantApprovedAt time.Time
	}{
		{"approving records the approval time", []DeviceStatus{DeviceApproved}, DeviceApproved, at(10)},
		{"revoking a pending device leaves approval time null", []DeviceStatus{DeviceRevoked}, DeviceRevoked, time.Time{}},
		{"revoking an approved device keeps its approval time", []DeviceStatus{DeviceApproved, DeviceRevoked}, DeviceRevoked, at(10)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := openTestStore(t)
			mustInsertDevices(t, s, device("d1"))
			for i, status := range tt.steps {
				if err := s.SetDeviceStatus(t.Context(), "d1", status, at(10+i)); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.Device(t.Context(), "d1")
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			if !got.ApprovedAt.Equal(tt.wantApprovedAt) {
				t.Fatalf("approved at = %v, want %v", got.ApprovedAt, tt.wantApprovedAt)
			}
		})
	}
}

func TestTouchDeviceSetsLastSeen(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	mustInsertDevices(t, s, device("d1"))
	if err := s.TouchDevice(t.Context(), "d1", at(42)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Device(t.Context(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastSeenAt.Equal(at(42)) {
		t.Fatalf("last seen = %v, want %v", got.LastSeenAt, at(42))
	}
}

func TestTimestampsKeepMillisecondPrecision(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	d := device("d1")
	d.CreatedAt = epoch.Add(123*time.Millisecond + 456*time.Microsecond)
	mustInsertDevices(t, s, d)
	got, err := s.Device(t.Context(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	want := epoch.Add(123 * time.Millisecond)
	if !got.CreatedAt.Equal(want) {
		t.Fatalf("created at = %v, want %v", got.CreatedAt, want)
	}
}
