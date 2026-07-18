package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
)

const (
	deviceTokenBytes   = 32
	pairingTokenBytes  = 16
	pairingSecretBytes = 16
	codeSpace          = 1_000_000
	unbiasedCodeLimit  = (1 << 32) / codeSpace * codeSpace
)

func NewToken() (plain string, hash []byte) {
	plain = randomToken(deviceTokenBytes)
	return plain, HashToken(plain)
}

func HashToken(plain string) []byte {
	sum := sha256.Sum256([]byte(plain))
	return sum[:]
}

func randomToken(byteCount int) string {
	return base64.RawURLEncoding.EncodeToString(randomBytes(byteCount))
}

func randomBytes(byteCount int) []byte {
	b := make([]byte, byteCount)
	rand.Read(b)
	return b
}

func randomCode() string {
	var b [4]byte
	for {
		rand.Read(b[:])
		n := binary.BigEndian.Uint32(b[:])
		if n < unbiasedCodeLimit {
			return fmt.Sprintf("%06d", n%codeSpace)
		}
	}
}
