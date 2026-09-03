package seal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestSmallFrameVectorsSealAndOpenBothWays(t *testing.T) {
	t.Parallel()
	v := loadVectors(t).Frames
	key := unhexKey(t, v.Key)
	nonce := unhex(t, v.Nonce)
	for _, tc := range v.Small {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			plain, frame := unhex(t, tc.Plain), unhex(t, tc.Frame)
			sealed, err := SealFrame(&key, nonce, tc.Index, plain)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(sealed, frame) {
				t.Fatalf("sealed %x, want %x", sealed, frame)
			}
			if got := binary.BigEndian.Uint32(frame); int(got) != len(plain) || len(frame) != len(plain)+frameHeaderSize+tagSize {
				t.Fatalf("frame header %d and length %d do not match plaintext of %d bytes", got, len(frame), len(plain))
			}
			opened := readAll(t, NewReader(&key, nonce, int64(tc.Index)*FrameSize, bytes.NewReader(frame)))
			if !bytes.Equal(opened, plain) {
				t.Fatalf("opened %x, want %x", opened, plain)
			}
		})
	}
}

func TestLargeFrameVectorsMatchByDigest(t *testing.T) {
	t.Parallel()
	v := loadVectors(t).Frames
	key := unhexKey(t, v.Key)
	nonce := unhex(t, v.Nonce)
	for _, tc := range v.Large {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			frame, err := SealFrame(&key, nonce, tc.Index, patternPlain(tc.PlainLength))
			if err != nil {
				t.Fatal(err)
			}
			if len(frame) != tc.FrameLength || sha256Of(frame) != tc.FrameSHA256 {
				t.Fatalf("frame of %d bytes with digest %s, want %d and %s", len(frame), sha256Of(frame), tc.FrameLength, tc.FrameSHA256)
			}
		})
	}
}

func TestStreamVectorCoversAllFramesAndTheResume(t *testing.T) {
	t.Parallel()
	v := loadVectors(t).Frames
	key := unhexKey(t, v.Key)
	nonce := unhex(t, v.Nonce)
	plain := patternPlain(v.Stream.PlainLength)
	frames := sealFrames(t, &key, nonce, 0, plain)
	if len(frames) != v.Stream.FrameCount {
		t.Fatalf("%d frames, want %d", len(frames), v.Stream.FrameCount)
	}
	body := concat(frames)
	if len(body) != v.Stream.BodyLength || sha256Of(body) != v.Stream.BodySHA256 {
		t.Fatalf("body of %d bytes with digest %s, want %d and %s", len(body), sha256Of(body), v.Stream.BodyLength, v.Stream.BodySHA256)
	}
	resume := concat(sealFrames(t, &key, nonce, uint64(v.Stream.ResumeOffset)/FrameSize, plain[v.Stream.ResumeOffset:]))
	if len(resume) != v.Stream.ResumeBodyLength || sha256Of(resume) != v.Stream.ResumeBodySHA256 {
		t.Fatalf("resume body of %d bytes with digest %s, want %d and %s", len(resume), sha256Of(resume), v.Stream.ResumeBodyLength, v.Stream.ResumeBodySHA256)
	}
	if got := readAll(t, NewReader(&key, nonce, v.Stream.ResumeOffset, bytes.NewReader(resume))); !bytes.Equal(got, plain[v.Stream.ResumeOffset:]) {
		t.Fatal("resume body did not decrypt to the plaintext tail")
	}
}

func TestFrameNonceIsUploadNonceThenBigEndianIndex(t *testing.T) {
	t.Parallel()
	nonce := bytes.Repeat([]byte{0xab}, UploadNonceSize)
	got := frameNonce(nonce, 1<<32+1)
	want := append(bytes.Repeat([]byte{0xab}, UploadNonceSize), 0, 0, 0, 1, 0, 0, 0, 1)
	if !bytes.Equal(got[:], want) {
		t.Fatalf("nonce %x, want %x", got, want)
	}
}

func TestFrameAtAnotherIndexOrUploadIsRejected(t *testing.T) {
	t.Parallel()
	key := seedKey("reorder key")
	nonce := seed("reorder nonce")[:UploadNonceSize]
	plain := patternPlain(FrameSize)
	frame, err := SealFrame(&key, nonce, 1, plain)
	if err != nil {
		t.Fatal(err)
	}
	otherKey := seedKey("other key")
	otherNonce := seed("other nonce")[:UploadNonceSize]
	cases := []struct {
		name   string
		key    *[keySize]byte
		nonce  []byte
		offset int64
	}{
		{"replayed one frame earlier", &key, nonce, 0},
		{"replayed one frame later", &key, nonce, 2 * FrameSize},
		{"replayed into another upload", &key, otherNonce, FrameSize},
		{"replayed under another session", &otherKey, nonce, FrameSize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := io.ReadAll(NewReader(tc.key, tc.nonce, tc.offset, bytes.NewReader(frame)))
			if !errors.Is(err, ErrFrame) {
				t.Fatalf("err %v, want ErrFrame", err)
			}
		})
	}
	if got := readAll(t, NewReader(&key, nonce, FrameSize, bytes.NewReader(frame))); !bytes.Equal(got, plain) {
		t.Fatal("the frame at its own index did not open")
	}
}

func TestSealFrameRejectsBadSizes(t *testing.T) {
	t.Parallel()
	key := seedKey("size key")
	nonce := seed("size nonce")[:UploadNonceSize]
	cases := []struct {
		name  string
		nonce []byte
		plain []byte
		want  error
	}{
		{"empty plaintext", nonce, nil, ErrFrame},
		{"plaintext above FrameSize", nonce, make([]byte, FrameSize+1), ErrFrame},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := SealFrame(&key, tc.nonce, 0, tc.plain); !errors.Is(err, tc.want) {
				t.Fatalf("err %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := SealFrame(&key, nonce[:UploadNonceSize-1], 0, []byte{1}); err == nil {
		t.Fatal("a short upload nonce was accepted")
	}
}

func patternPlain(length int) []byte {
	plain := make([]byte, length)
	for j := range plain {
		plain[j] = byte(j % 251)
	}
	return plain
}

func sealFrames(t *testing.T, key *[keySize]byte, nonce []byte, firstIndex uint64, plain []byte) [][]byte {
	t.Helper()
	var frames [][]byte
	for start := 0; start < len(plain); start += FrameSize {
		end := min(start+FrameSize, len(plain))
		frame, err := SealFrame(key, nonce, firstIndex+uint64(len(frames)), plain[start:end])
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, frame)
	}
	return frames
}

func concat(frames [][]byte) []byte {
	return bytes.Join(frames, nil)
}

func readAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
