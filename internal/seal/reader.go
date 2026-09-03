package seal

import (
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

const frameBufferSize = FrameSize + tagSize

var frameBuffers sync.Pool

type frameReader struct {
	aead    cipher.AEAD
	nonce   []byte
	body    io.Reader
	frame   *[frameBufferSize]byte
	pending []byte
	index   uint64
	skip    int
	err     error
}

func NewReader(key *[keySize]byte, nonce []byte, offset int64, body io.Reader) io.Reader {
	r := &frameReader{body: body, nonce: append([]byte(nil), nonce...)}
	switch {
	case len(nonce) != UploadNonceSize:
		r.err = fmt.Errorf("upload nonce is %d bytes: %w", len(nonce), ErrFrame)
	case offset < 0:
		r.err = fmt.Errorf("offset %d: %w", offset, ErrFrame)
	default:
		r.aead, r.err = newAEAD(key)
		r.index = uint64(offset) / FrameSize
		r.skip = int(offset % FrameSize)
	}
	return r
}

func (r *frameReader) Read(p []byte) (int, error) {
	if len(r.pending) == 0 {
		if err := r.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *frameReader) WriteTo(w io.Writer) (int64, error) {
	var total int64
	for {
		if len(r.pending) == 0 {
			if err := r.next(); err != nil {
				if errors.Is(err, io.EOF) {
					return total, nil
				}
				return total, err
			}
		}
		n, err := w.Write(r.pending)
		total += int64(n)
		r.pending = r.pending[n:]
		if err != nil {
			return total, err
		}
	}
}

func (r *frameReader) next() error {
	if r.err != nil {
		return r.err
	}
	for len(r.pending) == 0 {
		plain, err := r.readFrame()
		if err != nil {
			return r.end(err)
		}
		if r.skip > len(plain) {
			return r.end(fmt.Errorf("frame %d shorter than the resume offset: %w", r.index-1, ErrFrame))
		}
		r.pending = plain[r.skip:]
		r.skip = 0
	}
	return nil
}

func (r *frameReader) end(err error) error {
	r.err = err
	r.pending = nil
	if r.frame != nil {
		frameBuffers.Put(r.frame)
		r.frame = nil
	}
	return err
}

func (r *frameReader) readFrame() ([]byte, error) {
	if r.frame == nil {
		r.frame = pooledFrameBuffer()
	}
	header := r.frame[:frameHeaderSize]
	if _, err := io.ReadFull(r.body, header); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header)
	if length == 0 || length > FrameSize {
		return nil, fmt.Errorf("frame %d length %d: %w", r.index, length, ErrFrame)
	}
	sealed := r.frame[:int(length)+tagSize]
	if _, err := io.ReadFull(r.body, sealed); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	nonce := frameNonce(r.nonce, r.index)
	plain, err := r.aead.Open(sealed[:0], nonce[:], sealed, []byte(aadFrame))
	if err != nil {
		return nil, fmt.Errorf("open frame %d: %w", r.index, ErrFrame)
	}
	r.index++
	return plain, nil
}

func pooledFrameBuffer() *[frameBufferSize]byte {
	if frame, ok := frameBuffers.Get().(*[frameBufferSize]byte); ok {
		return frame
	}
	return new([frameBufferSize]byte)
}
