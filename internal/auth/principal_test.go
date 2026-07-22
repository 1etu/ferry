package auth

import (
	"testing"

	"github.com/1etu/ferry/internal/store"
)

func TestFromContextWithoutPrincipalIsNone(t *testing.T) {
	t.Parallel()
	if got := FromContext(t.Context()); got.Role != RoleNone || got.Device.ID != "" {
		t.Fatalf("got %+v, want the zero principal", got)
	}
}

func TestFromContextReturnsStoredPrincipal(t *testing.T) {
	t.Parallel()
	want := Principal{Role: RoleDevice, Device: store.Device{ID: "D1", Status: store.DeviceApproved}}
	got := FromContext(withPrincipal(t.Context(), want))
	if got.Role != want.Role || got.Device.ID != want.Device.ID {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPrincipalDeviceStatusPredicates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		principal    Principal
		wantApproved bool
		wantPending  bool
	}{
		{"approved device", Principal{Role: RoleDevice, Device: store.Device{Status: store.DeviceApproved}}, true, false},
		{"pending device", Principal{Role: RoleDevice, Device: store.Device{Status: store.DevicePending}}, false, true},
		{"owner is neither", Principal{Role: RoleOwner}, false, false},
		{"none is neither", Principal{}, false, false},
		{"device status without device role is ignored", Principal{Role: RoleNone, Device: store.Device{Status: store.DeviceApproved}}, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.principal.isApprovedDevice(); got != tc.wantApproved {
				t.Fatalf("isApprovedDevice = %v, want %v", got, tc.wantApproved)
			}
			if got := tc.principal.isPendingDevice(); got != tc.wantPending {
				t.Fatalf("isPendingDevice = %v, want %v", got, tc.wantPending)
			}
		})
	}
}
