package seal

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

const (
	labelProof   = "ferry-seal-proof"
	labelKey     = "ferry-seal-v1"
	labelConfirm = "ferry-seal-confirm"
	labelDevice  = "ferry-seal-device"
)

type Client struct {
	Public  []byte
	private [keySize]byte
}

func NewClient() (*Client, error) {
	var private [keySize]byte
	if _, err := rand.Read(private[:]); err != nil {
		return nil, fmt.Errorf("generate client key: %w", err)
	}
	return newClient(&private)
}

func newClient(private *[keySize]byte) (*Client, error) {
	public, err := curve25519.X25519(private[:], curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("derive client key: %w", err)
	}
	return &Client{Public: public, private: *private}, nil
}

func (c *Client) Proof(deviceSecret []byte) []byte {
	return Proof(deviceSecret, c.Public)
}

func (c *Client) Finish(deviceSecret, serverKey, confirm []byte) (key [keySize]byte, derivedSecret []byte, err error) {
	shared, err := sharedSecret(&c.private, serverKey)
	if err != nil {
		return key, nil, err
	}
	key, err = deriveKey(shared, deviceSecret, c.Public, serverKey)
	if err != nil {
		return key, nil, err
	}
	if !hmac.Equal(confirm, mac(key[:], labelConfirm, serverKey)) {
		return [keySize]byte{}, nil, ErrConfirm
	}
	if len(deviceSecret) > 0 {
		return key, nil, nil
	}
	derivedSecret, err = derive(shared, nil, labelDevice, c.Public, serverKey)
	if err != nil {
		return [keySize]byte{}, nil, err
	}
	return key, derivedSecret, nil
}

func handshake(deviceSecret, clientKey, proof []byte, serverPrivate *[keySize]byte) (Handshake, error) {
	if len(clientKey) != curve25519.PointSize {
		return Handshake{}, fmt.Errorf("client key is %d bytes: %w", len(clientKey), ErrPublicKey)
	}
	if err := verifyProof(deviceSecret, clientKey, proof); err != nil {
		return Handshake{}, err
	}
	serverKey, err := curve25519.X25519(serverPrivate[:], curve25519.Basepoint)
	if err != nil {
		return Handshake{}, fmt.Errorf("derive server key: %w", err)
	}
	shared, err := sharedSecret(serverPrivate, clientKey)
	if err != nil {
		return Handshake{}, err
	}
	key, err := deriveKey(shared, deviceSecret, clientKey, serverKey)
	if err != nil {
		return Handshake{}, err
	}
	result := Handshake{
		Session:   Session{Key: key},
		ServerKey: serverKey,
		Confirm:   mac(key[:], labelConfirm, serverKey),
	}
	if len(deviceSecret) > 0 {
		return result, nil
	}
	result.DeviceSecret, err = derive(shared, nil, labelDevice, clientKey, serverKey)
	if err != nil {
		return Handshake{}, err
	}
	return result, nil
}

func verifyProof(deviceSecret, clientKey, proof []byte) error {
	hasSecret := len(deviceSecret) > 0
	hasProof := len(proof) > 0
	if hasSecret != hasProof {
		return ErrProof
	}
	if hasSecret && !hmac.Equal(proof, Proof(deviceSecret, clientKey)) {
		return ErrProof
	}
	return nil
}

func Proof(deviceSecret, clientKey []byte) []byte {
	return mac(deviceSecret, labelProof, clientKey)
}

func DeviceSecret(pairingSecret []byte) ([]byte, error) {
	return derive(pairingSecret, nil, labelDevice)
}

func sharedSecret(private *[keySize]byte, peerPublic []byte) ([]byte, error) {
	if len(peerPublic) != curve25519.PointSize {
		return nil, fmt.Errorf("public key is %d bytes: %w", len(peerPublic), ErrPublicKey)
	}
	shared, err := curve25519.X25519(private[:], peerPublic)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPublicKey, err)
	}
	return shared, nil
}

func deriveKey(shared, deviceSecret, clientKey, serverKey []byte) ([keySize]byte, error) {
	var key [keySize]byte
	derived, err := derive(shared, deviceSecret, labelKey, clientKey, serverKey)
	if err != nil {
		return key, err
	}
	copy(key[:], derived)
	return key, nil
}

func derive(ikm, salt []byte, label string, parts ...[]byte) ([]byte, error) {
	info := []byte(label)
	for _, part := range parts {
		info = append(info, part...)
	}
	out := make([]byte, keySize)
	if _, err := io.ReadFull(hkdf.New(sha256.New, ikm, salt, info), out); err != nil {
		return nil, fmt.Errorf("derive %s: %w", label, err)
	}
	return out, nil
}

func mac(key []byte, label string, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(label))
	m.Write(data)
	return m.Sum(nil)
}
