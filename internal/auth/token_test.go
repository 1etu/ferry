package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"testing"
)

func TestNewTokenIsRandomBase64URLWithSHA256Hash(t *testing.T) {
	t.Parallel()
	plain, hash := NewToken()
	if len(plain) != 43 {
		t.Fatalf("token length %d, want 43", len(plain))
	}
	raw, err := base64.RawURLEncoding.DecodeString(plain)
	if err != nil {
		t.Fatalf("token is not unpadded base64url: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("token carries %d bytes, want 32", len(raw))
	}
	want := sha256.Sum256([]byte(plain))
	if !bytes.Equal(hash, want[:]) {
		t.Fatalf("hash %x, want %x", hash, want)
	}
	other, _ := NewToken()
	if other == plain {
		t.Fatal("two tokens are equal")
	}
}

func TestHashTokenMatchesSHA256(t *testing.T) {
	t.Parallel()
	got := hex.EncodeToString(HashToken("abc"))
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if !bytes.Equal(HashToken("abc"), HashToken("abc")) {
		t.Fatal("hash is not deterministic")
	}
}

func TestPairingTokenIs22Base64URLCharacters(t *testing.T) {
	t.Parallel()
	token := randomToken(pairingTokenBytes)
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`).MatchString(token) {
		t.Fatalf("pairing token %q is not 22 base64url characters", token)
	}
}

func TestRandomCodeIsSixDigitsWithLeadingZerosAllowed(t *testing.T) {
	t.Parallel()
	sixDigits := regexp.MustCompile(`^\d{6}$`)
	seen := make(map[string]bool)
	for range 2000 {
		code := randomCode()
		if !sixDigits.MatchString(code) {
			t.Fatalf("code %q is not six digits", code)
		}
		seen[code] = true
	}
	if len(seen) < 1900 {
		t.Fatalf("only %d distinct codes in 2000 draws", len(seen))
	}
}
