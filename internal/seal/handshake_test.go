package seal

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func TestHandshakeTranscriptsMatchTheVectorsOnBothSides(t *testing.T) {
	t.Parallel()
	for _, tc := range loadVectors(t).Handshakes {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			deviceSecret := unhex(t, tc.DeviceSecret)
			clientPrivate := unhexKey(t, tc.ClientPrivate)
			serverPrivate := unhexKey(t, tc.ServerPrivate)
			client, err := newClient(&clientPrivate)
			if err != nil {
				t.Fatal(err)
			}
			requireHex(t, "client public", client.Public, tc.ClientPublic)
			var proof []byte
			if len(deviceSecret) > 0 {
				proof = client.Proof(deviceSecret)
			}
			requireHex(t, "proof", proof, tc.Proof)
			hs, err := handshake(deviceSecret, client.Public, proof, &serverPrivate)
			if err != nil {
				t.Fatal(err)
			}
			requireHex(t, "server public", hs.ServerKey, tc.ServerPublic)
			requireHex(t, "key", hs.Session.Key[:], tc.Key)
			requireHex(t, "confirm", hs.Confirm, tc.Confirm)
			requireHex(t, "derived device secret", hs.DeviceSecret, tc.DerivedDeviceSecret)
			key, derived, err := client.Finish(deviceSecret, hs.ServerKey, hs.Confirm)
			if err != nil {
				t.Fatal(err)
			}
			requireHex(t, "client key", key[:], tc.Key)
			requireHex(t, "client derived device secret", derived, tc.DerivedDeviceSecret)
			for name, got := range map[string][2]string{
				"clientKey": {base64.RawURLEncoding.EncodeToString(client.Public), tc.ClientKeyBase64url},
				"proof":     {base64.RawURLEncoding.EncodeToString(proof), tc.ProofBase64url},
				"serverKey": {base64.RawURLEncoding.EncodeToString(hs.ServerKey), tc.ServerKeyBase64url},
				"confirm":   {base64.RawURLEncoding.EncodeToString(hs.Confirm), tc.ConfirmBase64url},
			} {
				if got[0] != got[1] {
					t.Fatalf("%s text %s, want %s", name, got[0], got[1])
				}
			}
		})
	}
}

func TestPairingDeviceSecretMatchesTheVector(t *testing.T) {
	t.Parallel()
	v := loadVectors(t).Pairing
	got, err := DeviceSecret(unhex(t, v.Secret))
	if err != nil {
		t.Fatal(err)
	}
	requireHex(t, "device secret", got, v.DeviceSecret)
}

func TestHandshakeRejectsWrongMissingAndUnexpectedProofs(t *testing.T) {
	t.Parallel()
	secret := seed("proof secret")
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	wrong := client.Proof(secret)
	wrong[0] ^= 0x01
	cases := []struct {
		name         string
		deviceSecret []byte
		proof        []byte
	}{
		{"flipped proof", secret, wrong},
		{"proof under another secret", secret, client.Proof(seed("another secret"))},
		{"proof over another client key", secret, stranger.Proof(secret)},
		{"missing proof when the device has a secret", secret, nil},
		{"empty proof when the device has a secret", secret, []byte{}},
		{"proof when the device has no secret", nil, client.Proof(secret)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sessions := NewSessions(fixedClock())
			if _, err := sessions.Handshake("device", tc.deviceSecret, client.Public, tc.proof); !errors.Is(err, ErrProof) {
				t.Fatalf("err %v, want ErrProof", err)
			}
			if _, ok := sessions.Lookup("device", ""); ok {
				t.Fatal("a rejected handshake left a session")
			}
		})
	}
}

func TestHandshakeRejectsMalformedAndLowOrderClientKeys(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		key  []byte
	}{
		{"31 bytes", make([]byte, 31)},
		{"33 bytes", make([]byte, 33)},
		{"empty", nil},
		{"all-zero low-order point", make([]byte, 32)},
		{"the point of order 8", append([]byte{0xe0, 0xeb, 0x7a, 0x7c, 0x3b, 0x41, 0xb8, 0xae, 0x16, 0x56, 0xe3, 0xfa, 0xf1, 0x9f, 0xc4, 0x6a}, []byte{0xda, 0x09, 0x8d, 0xeb, 0x9c, 0x32, 0xb1, 0xfd, 0x86, 0x62, 0x05, 0x16, 0x5f, 0x49, 0xb8, 0x00}...)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sessions := NewSessions(fixedClock())
			_, err := sessions.Handshake("device", nil, tc.key, nil)
			if !errors.Is(err, ErrPublicKey) {
				t.Fatalf("err %v, want ErrPublicKey", err)
			}
		})
	}
}

func TestClientFinishRejectsABadConfirmOrServerKey(t *testing.T) {
	t.Parallel()
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	hs, err := NewSessions(fixedClock()).Handshake("device", nil, client.Public, nil)
	if err != nil {
		t.Fatal(err)
	}
	flipped := append([]byte(nil), hs.Confirm...)
	flipped[5] ^= 0x10
	cases := []struct {
		name      string
		serverKey []byte
		confirm   []byte
		want      error
	}{
		{"flipped confirm", hs.ServerKey, flipped, ErrConfirm},
		{"empty confirm", hs.ServerKey, nil, ErrConfirm},
		{"confirm for another server key", bytes.Repeat([]byte{9}, 32), hs.Confirm, ErrConfirm},
		{"short server key", hs.ServerKey[:31], hs.Confirm, ErrPublicKey},
		{"low-order server key", make([]byte, 32), hs.Confirm, ErrPublicKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			key, derived, err := client.Finish(nil, tc.serverKey, tc.confirm)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err %v, want %v", err, tc.want)
			}
			if key != [keySize]byte{} || derived != nil {
				t.Fatal("a failed finish leaked key material")
			}
		})
	}
}

func TestFirstUseDerivesOneSecretThatAuthenticatesLaterHandshakes(t *testing.T) {
	t.Parallel()
	sessions := NewSessions(fixedClock())
	first, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	hs, err := sessions.Handshake("device", nil, first.Public, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, derived, err := first.Finish(nil, hs.ServerKey, hs.Confirm)
	if err != nil {
		t.Fatal(err)
	}
	if key != hs.Session.Key || !bytes.Equal(derived, hs.DeviceSecret) || len(derived) != keySize {
		t.Fatal("the two sides disagree on the key or the derived device secret")
	}
	second, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Handshake("device", derived, second.Public, nil); !errors.Is(err, ErrProof) {
		t.Fatalf("a later handshake without proof: err %v, want ErrProof", err)
	}
	later, err := sessions.Handshake("device", derived, second.Public, second.Proof(derived))
	if err != nil {
		t.Fatal(err)
	}
	if later.DeviceSecret != nil {
		t.Fatal("an authenticated handshake must not derive a device secret")
	}
	laterKey, none, err := second.Finish(derived, later.ServerKey, later.Confirm)
	if err != nil || laterKey != later.Session.Key || none != nil {
		t.Fatalf("finish: key match %v, derived %x, err %v", laterKey == later.Session.Key, none, err)
	}
	if laterKey == key {
		t.Fatal("two handshakes produced the same key")
	}
}

func requireHex(t *testing.T, name string, got []byte, wantHex string) {
	t.Helper()
	if want := unhex(t, wantHex); !bytes.Equal(got, want) {
		t.Fatalf("%s %x, want %x", name, got, want)
	}
}
