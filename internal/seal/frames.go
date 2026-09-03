package seal

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	aadFrame        = "ferry-frame"
	frameHeaderSize = 4
	frameNonceSize  = chacha20poly1305.NonceSizeX
)

func SealFrame(key *[keySize]byte, nonce []byte, index uint64, plain []byte) ([]byte, error) {
	if len(nonce) != UploadNonceSize {
		return nil, fmt.Errorf("upload nonce is %d bytes, want %d", len(nonce), UploadNonceSize)
	}
	length := len(plain)
	if length == 0 || length > FrameSize {
		return nil, fmt.Errorf("frame %d plaintext is %d bytes: %w", index, length, ErrFrame)
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, frameHeaderSize, frameHeaderSize+length+tagSize)
	binary.BigEndian.PutUint32(frame, uint32(length))
	full := frameNonce(nonce, index)
	return aead.Seal(frame, full[:], plain, []byte(aadFrame)), nil
}

func frameNonce(uploadNonce []byte, index uint64) [frameNonceSize]byte {
	var nonce [frameNonceSize]byte
	copy(nonce[:UploadNonceSize], uploadNonce)
	binary.BigEndian.PutUint64(nonce[UploadNonceSize:], index)
	return nonce
}
