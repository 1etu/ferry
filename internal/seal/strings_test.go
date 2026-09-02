package seal

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func TestSealedStringVectorsOpenAndSealBothWays(t *testing.T) {
	t.Parallel()
	for _, tc := range loadVectors(t).Strings {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			key := unhexKey(t, tc.Key)
			sealed := unhex(t, tc.Sealed)
			got, err := sealWithNonce(&key, unhex(t, tc.Nonce), []byte(tc.Plain))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, sealed) {
				t.Fatalf("sealed %x, want %x", got, sealed)
			}
			if text := base64.RawURLEncoding.EncodeToString(sealed); text != tc.SealedBase64url {
				t.Fatalf("text %s, want %s", text, tc.SealedBase64url)
			}
			opened, err := Open(&key, sealed)
			if err != nil || string(opened) != tc.Plain {
				t.Fatalf("opened %q, err %v, want %q", opened, err, tc.Plain)
			}
			plain, err := OpenString(&key, tc.SealedBase64url)
			if err != nil || plain != tc.Plain {
				t.Fatalf("opened text %q, err %v, want %q", plain, err, tc.Plain)
			}
			fresh, err := SealString(&key, tc.Plain)
			if err != nil {
				t.Fatal(err)
			}
			if again, err := OpenString(&key, fresh); err != nil || again != tc.Plain {
				t.Fatalf("fresh seal opened to %q, err %v", again, err)
			}
		})
	}
}

func TestOpenRejectsTamperingTruncationAndTheWrongKey(t *testing.T) {
	t.Parallel()
	key := seedKey("string tamper key")
	other := seedKey("string other key")
	sealed, err := Seal(&key, []byte("Report.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	flip := func(at int) []byte {
		copied := append([]byte(nil), sealed...)
		copied[at] ^= 0x80
		return copied
	}
	cases := []struct {
		name   string
		key    *[keySize]byte
		sealed []byte
	}{
		{"flipped nonce byte", &key, flip(0)},
		{"flipped ciphertext byte", &key, flip(stringNonceSize)},
		{"flipped tag byte", &key, flip(len(sealed) - 1)},
		{"truncated by one byte", &key, sealed[:len(sealed)-1]},
		{"nonce and tag only", &key, sealed[:minSealedSize]},
		{"too short to hold a tag", &key, sealed[:minSealedSize-1]},
		{"empty", &key, nil},
		{"wrong key", &other, sealed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Open(tc.key, tc.sealed); !errors.Is(err, ErrString) {
				t.Fatalf("err %v, want ErrString", err)
			}
		})
	}
}

func TestOpenStringRejectsPaddingAndStandardAlphabet(t *testing.T) {
	t.Parallel()
	key := seedKey("string encoding key")
	sealed, err := Seal(&key, []byte("a/b+"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sealed)%3 == 0 {
		t.Fatalf("sealed length %d needs no padding, so the case proves nothing", len(sealed))
	}
	cases := []struct {
		name string
		text string
	}{
		{"padded", base64.URLEncoding.EncodeToString(sealed)},
		{"standard alphabet", base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{0xfb, 0xff}, 24))},
		{"not base64", "not base64!"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := OpenString(&key, tc.text); !errors.Is(err, ErrString) {
				t.Fatalf("err %v, want ErrString", err)
			}
		})
	}
}

func TestSealUsesAFreshNonceAndOpensUnderTheSameKey(t *testing.T) {
	t.Parallel()
	key := seedKey("string nonce key")
	first, err := Seal(&key, []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Seal(&key, []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first[:stringNonceSize], second[:stringNonceSize]) {
		t.Fatal("two seals shared a nonce")
	}
	if len(first) != stringNonceSize+len("same")+tagSize {
		t.Fatalf("sealed length %d", len(first))
	}
	for _, sealed := range [][]byte{first, second} {
		if plain, err := Open(&key, sealed); err != nil || string(plain) != "same" {
			t.Fatalf("opened %q, err %v", plain, err)
		}
	}
}
