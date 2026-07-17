package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"

	"github.com/1etu/ferry/internal/store"
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
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	var c contract
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatalf("parse contract: %v", err)
	}
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
	got := make([]string, 0, len(ErrorCodes()))
	for _, code := range ErrorCodes() {
		got = append(got, string(code))
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("error codes differ from api/openapi.yaml (-contract +go):\n%s", diff)
	}
}

func TestSchemasMatchContract(t *testing.T) {
	t.Parallel()
	now := time.Now()
	fullDevice := Device{ID: "d", Name: "iPhone", Status: store.DeviceApproved, CreatedAt: Timestamp(now), ApprovedAt: Timestamp(now), LastSeenAt: Timestamp(now)}
	tests := []struct {
		schema string
		full   any
		empty  any
	}{
		{"Error", NewError(CodeInternal, "boom"), Error{}},
		{"Health", Health{App: AppName, Name: "pc", Version: "1"}, Health{}},
		{"Session", Session{Role: RoleDevice, Device: &fullDevice, Server: ServerInfo{}}, Session{}},
		{"PairRequest", PairRequest{Token: "t", Code: "123456", Name: "iPhone", HasSecret: true}, PairRequest{}},
		{"Pairing", Pairing{QRURL: "q", LocalURL: "l", Code: "123456", ExpiresAt: Timestamp(now)}, Pairing{}},
		{"Device", fullDevice, Device{}},
		{"Transfer", Transfer{ID: "t", Error: CodeExpired, FileID: "f"}, Transfer{}},
		{"OfferedFile", OfferedFile{ID: "f"}, OfferedFile{}},
		{"OfferRequest", OfferRequest{Paths: []string{"a"}}, OfferRequest{}},
		{"SealRequest", SealRequest{ClientKey: "k", Proof: "p"}, SealRequest{}},
		{"SealResponse", SealResponse{SessionID: "s", ServerKey: "k", Confirm: "c"}, SealResponse{}},
		{"Settings", Settings{Name: "pc", ReceivedDir: "D:/Ferry", StartAtLogin: true, CheckUpdates: true}, Settings{}},
		{"SettingsPatch", fullSettingsPatch(), SettingsPatch{}},
		{"UpdateStatus", UpdateStatus{Current: "1.0.0", Available: "1.1.0", State: "failed", CheckedAt: Timestamp(now), Error: "boom"}, UpdateStatus{}},
		{"Network", Network{Firewall: FirewallAllowed, Profile: "private"}, Network{}},
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
			if diff := cmp.Diff(properties, jsonKeys(t, tc.full)); diff != "" {
				t.Fatalf("full value keys differ from contract properties (-contract +go):\n%s", diff)
			}
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

func fullSettingsPatch() SettingsPatch {
	name, dir, isOn := "pc", "D:/Ferry", true
	return SettingsPatch{Name: &name, ReceivedDir: &dir, StartAtLogin: &isOn, CheckUpdates: &isOn}
}

func TestTimestampIsUTCWithMilliseconds(t *testing.T) {
	t.Parallel()
	zone := time.FixedZone("CEST", 2*60*60)
	at := time.Date(2026, 10, 2, 16, 3, 7, 123_456_000, zone)
	if got := Timestamp(at); got != "2026-10-02T14:03:07.123Z" {
		t.Fatalf("got %q", got)
	}
	if got := OptionalTimestamp(time.Time{}); got != "" {
		t.Fatalf("zero time encoded as %q, want empty", got)
	}
	if got := OptionalTimestamp(at); got != Timestamp(at) {
		t.Fatalf("got %q", got)
	}
}

func TestTransferFromOmitsEmptyOptionals(t *testing.T) {
	t.Parallel()
	at := time.UnixMilli(1_790_000_000_000)
	encoded, err := json.Marshal(TransferFrom(store.Transfer{
		ID: "t1", DeviceID: "d1", Direction: store.DirectionIn, Name: "a.jpg", Size: 10, Done: 4,
		Status: store.TransferActive, CreatedAt: at, UpdatedAt: at,
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"t1","deviceId":"d1","direction":"in","name":"a.jpg","size":10,"done":4,"status":"active",` +
		`"createdAt":"2026-09-21T14:13:20.000Z","updatedAt":"2026-09-21T14:13:20.000Z"}`
	if string(encoded) != want {
		t.Fatalf("got  %s\nwant %s", encoded, want)
	}
}

func TestDeviceFromOmitsNullTimestamps(t *testing.T) {
	t.Parallel()
	keys := jsonKeys(t, DeviceFrom(store.Device{ID: "d1", Name: "iPhone", Status: store.DevicePending, CreatedAt: time.Now()}))
	if slices.Contains(keys, "approvedAt") || slices.Contains(keys, "lastSeenAt") {
		t.Fatalf("null timestamps encoded: %v", keys)
	}
}

func TestPairingURLCarriesTheTokenAndTheSecretInTheFragment(t *testing.T) {
	t.Parallel()
	secret := []byte("Boatsarenice!!!!")
	want := "http://192.168.1.23:8080/?pair=k3bJz9xQ2mL8vN4pR7tW1a#s=Qm9hdHNhcmVuaWNlISEhIQ"
	if got := PairingURL("http://192.168.1.23:8080", "k3bJz9xQ2mL8vN4pR7tW1a", secret); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWithSealedNamesSealsOnlyTheName(t *testing.T) {
	t.Parallel()
	seal := func(plain string) (string, bool) { return "sealed:" + plain, true }
	transfer := Transfer{ID: "t1", Name: "a.jpg", Size: 3}
	file := OfferedFile{ID: "f1", Name: "b.pdf", Size: 4}
	tests := []struct {
		name  string
		value interface {
			WithSealedNames(func(string) (string, bool)) (any, bool)
		}
		want any
	}{
		{"transfer", transfer, Transfer{ID: "t1", Name: "sealed:a.jpg", Size: 3}},
		{"offered file", file, OfferedFile{ID: "f1", Name: "sealed:b.pdf", Size: 4}},
		{"file change", FileChange{Action: FileAdded, File: file}, FileChange{Action: FileAdded, File: OfferedFile{ID: "f1", Name: "sealed:b.pdf", Size: 4}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := tc.value.WithSealedNames(seal)
			if !ok {
				t.Fatal("sealing reported failure")
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("sealed payload differs (-want +got):\n%s", diff)
			}
			if _, ok := tc.value.WithSealedNames(func(string) (string, bool) { return "", false }); ok {
				t.Fatal("a failed sealer still produced a payload")
			}
		})
	}
	if transfer.Name != "a.jpg" || file.Name != "b.pdf" {
		t.Fatal("sealing changed the plain values")
	}
}
