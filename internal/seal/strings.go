package seal

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	aadName         = "ferry-name"
	stringNonceSize = chacha20poly1305.NonceSizeX
	tagSize         = chacha20poly1305.Overhead
	minSealedSize   = stringNonceSize + tagSize
)

func Seal(key *[keySize]byte, plain []byte) ([]byte, error) {
	var nonce [stringNonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("generate string nonce: %w", err)
	}
	return sealWithNonce(key, nonce[:], plain)
}

func sealWithNonce(key *[keySize]byte, nonce, plain []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	sealed := make([]byte, stringNonceSize, stringNonceSize+len(plain)+tagSize)
	copy(sealed, nonce)
	return aead.Seal(sealed, sealed[:stringNonceSize], plain, []byte(aadName)), nil
}

func Open(key *[keySize]byte, sealed []byte) ([]byte, error) {
	if len(sealed) < minSealedSize {
		return nil, fmt.Errorf("sealed string is %d bytes: %w", len(sealed), ErrString)
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, sealed[:stringNonceSize], sealed[stringNonceSize:], []byte(aadName))
	if err != nil {
		return nil, ErrString
	}
	return plain, nil
}

func SealString(key *[keySize]byte, plain string) (string, error) {
	sealed, err := Seal(key, []byte(plain))
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func OpenString(key *[keySize]byte, sealed string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		return "", fmt.Errorf("decode sealed string: %w", ErrString)
	}
	plain, err := Open(key, raw)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func newAEAD(key *[keySize]byte) (cipher.AEAD, error) {
	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		return nil, fmt.Errorf("new xchacha20poly1305: %w", err)
	}
	return aead, nil
}
