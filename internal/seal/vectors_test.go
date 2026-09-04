package seal

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"golang.org/x/crypto/curve25519"
)

const vectorsPath = "testdata/vectors.json"

type vectors struct {
	Notes      []string          `json:"notes"`
	Pairing    pairingVector     `json:"pairing"`
	Handshakes []handshakeVector `json:"handshakes"`
	Strings    []stringVector    `json:"strings"`
	Frames     frameVectors      `json:"frames"`
}

type pairingVector struct {
	Secret       string `json:"secret"`
	DeviceSecret string `json:"deviceSecret"`
}

type handshakeVector struct {
	Name                string `json:"name"`
	DeviceSecret        string `json:"deviceSecret"`
	ClientPrivate       string `json:"clientPrivate"`
	ClientPublic        string `json:"clientPublic"`
	ClientKeyBase64url  string `json:"clientKeyBase64url"`
	Proof               string `json:"proof"`
	ProofBase64url      string `json:"proofBase64url"`
	ServerPrivate       string `json:"serverPrivate"`
	ServerPublic        string `json:"serverPublic"`
	ServerKeyBase64url  string `json:"serverKeyBase64url"`
	Shared              string `json:"shared"`
	Key                 string `json:"key"`
	Confirm             string `json:"confirm"`
	ConfirmBase64url    string `json:"confirmBase64url"`
	DerivedDeviceSecret string `json:"derivedDeviceSecret"`
}

type stringVector struct {
	Name            string `json:"name"`
	Key             string `json:"key"`
	Nonce           string `json:"nonce"`
	Plain           string `json:"plain"`
	Sealed          string `json:"sealed"`
	SealedBase64url string `json:"sealedBase64url"`
}

type frameVectors struct {
	Key          string             `json:"key"`
	Nonce        string             `json:"nonce"`
	PlainPattern string             `json:"plainPattern"`
	Small        []frameVector      `json:"small"`
	Large        []largeFrameVector `json:"large"`
	Stream       streamVector       `json:"stream"`
}

type frameVector struct {
	Name  string `json:"name"`
	Index uint64 `json:"index"`
	Plain string `json:"plain"`
	Frame string `json:"frame"`
}

type largeFrameVector struct {
	Name        string `json:"name"`
	Index       uint64 `json:"index"`
	PlainLength int    `json:"plainLength"`
	FrameLength int    `json:"frameLength"`
	FrameSHA256 string `json:"frameSha256"`
}

type streamVector struct {
	PlainLength      int    `json:"plainLength"`
	FrameCount       int    `json:"frameCount"`
	BodyLength       int    `json:"bodyLength"`
	BodySHA256       string `json:"bodySha256"`
	ResumeOffset     int64  `json:"resumeOffset"`
	ResumeBodyLength int    `json:"resumeBodyLength"`
	ResumeBodySHA256 string `json:"resumeBodySha256"`
}

func TestVectorsFileMatchesTheImplementation(t *testing.T) {
	want, err := json.MarshalIndent(buildVectors(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if os.Getenv("FERRY_WRITE_VECTORS") == "1" {
		if err := os.WriteFile(vectorsPath, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("testdata/vectors.json is stale; regenerate with FERRY_WRITE_VECTORS=1 go test ./internal/seal")
	}
}

func loadVectors(t *testing.T) vectors {
	t.Helper()
	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func buildVectors(t *testing.T) vectors {
	t.Helper()
	pairingSecret := seed("pairing secret")[:16]
	pairingDevice := mustBytes(t)(DeviceSecret(pairingSecret))
	return vectors{
		Notes: []string{
			"Byte strings are lowercase hex; fields ending in Base64url carry the wire text (RFC 4648 base64url, no padding).",
			"Normative layout: docs/architecture.md section 8.5. Regenerate: FERRY_WRITE_VECTORS=1 go test ./internal/seal",
		},
		Pairing: pairingVector{Secret: hexOf(pairingSecret), DeviceSecret: hexOf(pairingDevice)},
		Handshakes: []handshakeVector{
			buildHandshake(t, "with device secret", pairingDevice),
			buildHandshake(t, "trust on first use", nil),
		},
		Strings: []stringVector{
			buildString(t, "ascii", "IMG_0001.HEIC"),
			buildString(t, "unicode", "Fähre – 渡し船 🚢.mov"),
			buildString(t, "empty", ""),
		},
		Frames: buildFrames(t),
	}
}

func buildHandshake(t *testing.T, name string, deviceSecret []byte) handshakeVector {
	t.Helper()
	var clientPrivate, serverPrivate [keySize]byte
	copy(clientPrivate[:], seed(name+" client"))
	copy(serverPrivate[:], seed(name+" server"))
	client := mustClient(t)(newClient(&clientPrivate))
	var proof []byte
	if deviceSecret != nil {
		proof = client.Proof(deviceSecret)
	}
	hs, err := handshake(deviceSecret, client.Public, proof, &serverPrivate)
	if err != nil {
		t.Fatal(err)
	}
	shared := mustBytes(t)(curve25519.X25519(clientPrivate[:], hs.ServerKey))
	return handshakeVector{
		Name:                name,
		DeviceSecret:        hexOf(deviceSecret),
		ClientPrivate:       hexOf(clientPrivate[:]),
		ClientPublic:        hexOf(client.Public),
		ClientKeyBase64url:  base64.RawURLEncoding.EncodeToString(client.Public),
		Proof:               hexOf(proof),
		ProofBase64url:      base64.RawURLEncoding.EncodeToString(proof),
		ServerPrivate:       hexOf(serverPrivate[:]),
		ServerPublic:        hexOf(hs.ServerKey),
		ServerKeyBase64url:  base64.RawURLEncoding.EncodeToString(hs.ServerKey),
		Shared:              hexOf(shared),
		Key:                 hexOf(hs.Session.Key[:]),
		Confirm:             hexOf(hs.Confirm),
		ConfirmBase64url:    base64.RawURLEncoding.EncodeToString(hs.Confirm),
		DerivedDeviceSecret: hexOf(hs.DeviceSecret),
	}
}

func buildString(t *testing.T, name, plain string) stringVector {
	t.Helper()
	key := seedKey("string key")
	nonce := seed("string nonce " + name)[:stringNonceSize]
	sealed := mustBytes(t)(sealWithNonce(&key, nonce, []byte(plain)))
	return stringVector{
		Name:            name,
		Key:             hexOf(key[:]),
		Nonce:           hexOf(nonce),
		Plain:           plain,
		Sealed:          hexOf(sealed),
		SealedBase64url: base64.RawURLEncoding.EncodeToString(sealed),
	}
}

func buildFrames(t *testing.T) frameVectors {
	t.Helper()
	key := seedKey("frame key")
	nonce := seed("upload nonce")[:UploadNonceSize]
	small := func(name string, index uint64, plain []byte) frameVector {
		frame := mustBytes(t)(SealFrame(&key, nonce, index, plain))
		return frameVector{Name: name, Index: index, Plain: hexOf(plain), Frame: hexOf(frame)}
	}
	large := func(name string, index uint64, length int) largeFrameVector {
		frame := mustBytes(t)(SealFrame(&key, nonce, index, patternPlain(length)))
		return largeFrameVector{Name: name, Index: index, PlainLength: length, FrameLength: len(frame), FrameSHA256: sha256Of(frame)}
	}
	plain := patternPlain(FrameSize*2 + FrameSize/2)
	body := concat(sealFrames(t, &key, nonce, 0, plain))
	resume := concat(sealFrames(t, &key, nonce, 1, plain[FrameSize:]))
	return frameVectors{
		Key:          hexOf(key[:]),
		Nonce:        hexOf(nonce),
		PlainPattern: "byte j of a large plaintext is j mod 251",
		Small: []frameVector{
			small("one byte at index 0", 0, []byte{0x42}),
			small("seventeen bytes at index 1", 1, seed("seventeen")[:17]),
			small("one hundred pattern bytes at an index above 32 bits", 1<<32+1, patternPlain(100)),
		},
		Large: []largeFrameVector{
			large("full frame at index 0", 0, FrameSize),
			large("full frame at index 7", 7, FrameSize),
		},
		Stream: streamVector{
			PlainLength:      len(plain),
			FrameCount:       3,
			BodyLength:       len(body),
			BodySHA256:       sha256Of(body),
			ResumeOffset:     FrameSize,
			ResumeBodyLength: len(resume),
			ResumeBodySHA256: sha256Of(resume),
		},
	}
}

func seed(label string) []byte {
	sum := sha256.Sum256([]byte("ferry vectors: " + label))
	return sum[:]
}

func seedKey(label string) [keySize]byte {
	var key [keySize]byte
	copy(key[:], seed(label))
	return key
}

func hexOf(b []byte) string { return hex.EncodeToString(b) }

func sha256Of(b []byte) string {
	sum := sha256.Sum256(b)
	return hexOf(sum[:])
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex %q: %v", s, err)
	}
	return b
}

func unhexKey(t *testing.T, s string) [keySize]byte {
	t.Helper()
	var key [keySize]byte
	if n := copy(key[:], unhex(t, s)); n != keySize {
		t.Fatalf("key %q is %d bytes", s, n)
	}
	return key
}

func mustBytes(t *testing.T) func([]byte, error) []byte {
	t.Helper()
	return func(b []byte, err error) []byte {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
}

func mustClient(t *testing.T) func(*Client, error) *Client {
	t.Helper()
	return func(c *Client, err error) *Client {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
}
