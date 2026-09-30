package api_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/store"
	"github.com/1etu/ferry/tests/kit"
)

type contractSchema struct {
	Enum       []string       `yaml:"enum"`
	Required   []string       `yaml:"required"`
	Properties map[string]any `yaml:"properties"`
}

type contract struct {
	Components struct {
		Schemas map[string]contractSchema `yaml:"schemas"`
	} `yaml:"components"`
}

func loadContract(t *testing.T) contract {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	kit.NoError(t, err, "read contract")
	var c contract
	kit.NoError(t, yaml.Unmarshal(raw, &c), "parse contract")
	return c
}

func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode %T: %v", v, err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatalf("decode %T: %v", v, err)
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func TestErrorCodesMatchContract(t *testing.T) {
	t.Parallel()
	want := loadContract(t).Components.Schemas["ErrorCode"].Enum
	got := make([]string, 0, len(api.ErrorCodes()))
	for _, code := range api.ErrorCodes() {
		got = append(got, string(code))
	}
	kit.Equal(t, got, want)
}

func TestSchemasMatchContract(t *testing.T) {
	t.Parallel()
	now := time.Now()
	fullDevice := api.Device{ID: "d", Name: "iPhone", Status: store.DeviceApproved, CreatedAt: api.Timestamp(now), ApprovedAt: api.Timestamp(now), LastSeenAt: api.Timestamp(now)}
	tests := []struct {
		schema string
		full   any
		empty  any
	}{
		{"Error", api.NewError(api.CodeInternal, "boom"), api.Error{}},
		{"Health", api.Health{App: api.AppName, Name: "pc", Version: "1"}, api.Health{}},
		{"Session", api.Session{Role: api.RoleDevice, Device: &fullDevice, Server: api.ServerInfo{}}, api.Session{}},
		{"PairRequest", api.PairRequest{Token: "t", Code: "123456", Name: "iPhone", HasSecret: true}, api.PairRequest{}},
		{"Pairing", api.Pairing{QRURL: "q", LocalURL: "l", Code: "123456", ExpiresAt: api.Timestamp(now)}, api.Pairing{}},
		{"Device", fullDevice, api.Device{}},
		{"Transfer", api.Transfer{ID: "t", Error: api.CodeExpired, FileID: "f"}, api.Transfer{}},
		{"OfferedFile", api.OfferedFile{ID: "f"}, api.OfferedFile{}},
		{"OfferRequest", api.OfferRequest{Paths: []string{"a"}}, api.OfferRequest{}},
		{"SealRequest", api.SealRequest{ClientKey: "k", Proof: "p"}, api.SealRequest{}},
		{"SealResponse", api.SealResponse{SessionID: "s", ServerKey: "k", Confirm: "c"}, api.SealResponse{}},
		{"Settings", api.Settings{Name: "pc", ReceivedDir: "D:/Ferry", StartAtLogin: true, CheckUpdates: true}, api.Settings{}},
		{"SettingsPatch", fullSettingsPatch(), api.SettingsPatch{}},
		{"UpdateStatus", api.UpdateStatus{Current: "1.0.0", Available: "1.1.0", State: "failed", CheckedAt: api.Timestamp(now), Error: "boom"}, api.UpdateStatus{}},
		{"Network", api.Network{Firewall: api.FirewallAllowed, Profile: "private"}, api.Network{}},
	}
	schemas := loadContract(t).Components.Schemas
	for _, tc := range tests {
		t.Run(tc.schema, func(t *testing.T) {
			t.Parallel()
			schema, ok := schemas[tc.schema]
			if !ok {
				t.Fatalf("schema %s missing from api/openapi.yaml", tc.schema)
			}
			properties := make([]string, 0, len(schema.Properties))
			for name := range schema.Properties {
				properties = append(properties, name)
			}
			slices.Sort(properties)
			kit.Equal(t, jsonKeys(t, tc.full), properties)
			required := slices.Sorted(slices.Values(schema.Required))
			emptyKeys := jsonKeys(t, tc.empty)
			for _, name := range required {
				if !slices.Contains(emptyKeys, name) {
					t.Fatalf("required property %q is omitted from an empty %s", name, tc.schema)
				}
			}
		})
	}
}

func fullSettingsPatch() api.SettingsPatch {
	name, dir, isOn := "pc", "D:/Ferry", true
	return api.SettingsPatch{Name: &name, ReceivedDir: &dir, StartAtLogin: &isOn, CheckUpdates: &isOn}
}

func TestTimestampIsUTCWithMilliseconds(t *testing.T) {
	t.Parallel()
	zone := time.FixedZone("CEST", 2*60*60)
	at := time.Date(2026, 10, 2, 16, 3, 7, 123_456_000, zone)
	if got := api.Timestamp(at); got != "2026-10-02T14:03:07.123Z" {
		t.Fatalf("got %q", got)
	}
	if got := api.OptionalTimestamp(time.Time{}); got != "" {
		t.Fatalf("zero time encoded as %q, want empty", got)
	}
	if got := api.OptionalTimestamp(at); got != api.Timestamp(at) {
		t.Fatalf("got %q", got)
	}
}

func TestTransferFromOmitsEmptyOptionals(t *testing.T) {
	t.Parallel()
	at := time.UnixMilli(1_790_000_000_000)
	encoded, err := json.Marshal(api.TransferFrom(store.Transfer{
		ID: "t1", DeviceID: "d1", Direction: store.DirectionIn, Name: "a.jpg", Size: 10, Done: 4,
		Status: store.TransferActive, CreatedAt: at, UpdatedAt: at,
	}))
	kit.NoError(t, err)
	want := `{"id":"t1","deviceId":"d1","direction":"in","name":"a.jpg","size":10,"done":4,"status":"active",` +
		`"createdAt":"2026-09-21T14:13:20.000Z","updatedAt":"2026-09-21T14:13:20.000Z"}`
	if string(encoded) != want {
		t.Fatalf("got  %s\nwant %s", encoded, want)
	}
}

func TestDeviceFromOmitsNullTimestamps(t *testing.T) {
	t.Parallel()
	keys := jsonKeys(t, api.DeviceFrom(store.Device{ID: "d1", Name: "iPhone", Status: store.DevicePending, CreatedAt: time.Now()}))
	if slices.Contains(keys, "approvedAt") || slices.Contains(keys, "lastSeenAt") {
		t.Fatalf("null timestamps encoded: %v", keys)
	}
}

func TestPairingURLCarriesTheTokenAndTheSecretInTheFragment(t *testing.T) {
	t.Parallel()
	secret := []byte("Boatsarenice!!!!")
	want := "http://192.168.1.23:8080/?pair=k3bJz9xQ2mL8vN4pR7tW1a#s=Qm9hdHNhcmVuaWNlISEhIQ"
	if got := api.PairingURL("http://192.168.1.23:8080", "k3bJz9xQ2mL8vN4pR7tW1a", secret); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWithSealedNamesSealsOnlyTheName(t *testing.T) {
	t.Parallel()
	seal := func(plain string) (string, bool) { return "sealed:" + plain, true }
	transfer := api.Transfer{ID: "t1", Name: "a.jpg", Size: 3}
	file := api.OfferedFile{ID: "f1", Name: "b.pdf", Size: 4}
	tests := []struct {
		name  string
		value interface {
			WithSealedNames(func(string) (string, bool)) (any, bool)
		}
		want any
	}{
		{"transfer", transfer, api.Transfer{ID: "t1", Name: "sealed:a.jpg", Size: 3}},
		{"offered file", file, api.OfferedFile{ID: "f1", Name: "sealed:b.pdf", Size: 4}},
		{"file change", api.FileChange{Action: api.FileAdded, File: file}, api.FileChange{Action: api.FileAdded, File: api.OfferedFile{ID: "f1", Name: "sealed:b.pdf", Size: 4}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := tc.value.WithSealedNames(seal)
			if !ok {
				t.Fatal("sealing reported failure")
			}
			kit.Equal(t, got, tc.want)
			if _, ok := tc.value.WithSealedNames(func(string) (string, bool) { return "", false }); ok {
				t.Fatal("a failed sealer still produced a payload")
			}
		})
	}
	if transfer.Name != "a.jpg" || file.Name != "b.pdf" {
		t.Fatal("sealing changed the plain values")
	}
}
