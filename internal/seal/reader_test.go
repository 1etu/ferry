package seal

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"testing"
)

type readerFixture struct {
	key    [keySize]byte
	nonce  []byte
	plain  []byte
	frames [][]byte
}

func newReaderFixture(t *testing.T, length int) readerFixture {
	t.Helper()
	f := readerFixture{key: seedKey("reader key"), nonce: seed("reader nonce")[:UploadNonceSize], plain: patternPlain(length)}
	f.frames = sealFrames(t, &f.key, f.nonce, 0, f.plain)
	return f
}

func (f readerFixture) reader(offset int64, body io.Reader) io.Reader {
	return NewReader(&f.key, f.nonce, offset, body)
}

type writeRecorder struct {
	buf   bytes.Buffer
	sizes []int
}

func (w *writeRecorder) Write(p []byte) (int, error) {
	w.sizes = append(w.sizes, len(p))
	return w.buf.Write(p)
}

func TestReaderRoundTripsOverAPipe(t *testing.T) {
	t.Parallel()
	f := newReaderFixture(t, 2*FrameSize+FrameSize/2)
	pr, pw := io.Pipe()
	go func() {
		body := concat(f.frames)
		for len(body) > 0 {
			n := min(7919, len(body))
			if _, err := pw.Write(body[:n]); err != nil {
				pw.CloseWithError(err)
				return
			}
			body = body[n:]
		}
		pw.Close()
	}()
	var out bytes.Buffer
	if _, err := io.Copy(&out, f.reader(0, pr)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), f.plain) {
		t.Fatal("plaintext differs after the round trip")
	}
}

func TestReaderKeepsTheVerifiedPrefixAndStopsOnATamperedTag(t *testing.T) {
	t.Parallel()
	f := newReaderFixture(t, 3*FrameSize)
	body := concat(f.frames)
	body[len(f.frames[0])+len(f.frames[1])-1] ^= 0x01
	var out writeRecorder
	n, err := writeTo(t, f.reader(0, bytes.NewReader(body)), &out)
	if !errors.Is(err, ErrFrame) {
		t.Fatalf("err %v, want ErrFrame", err)
	}
	if n != FrameSize || !bytes.Equal(out.buf.Bytes(), f.plain[:FrameSize]) {
		t.Fatalf("wrote %d bytes, want exactly the verified first frame", n)
	}
	if _, err := f.reader(0, bytes.NewReader(body)).Read(make([]byte, 1)); err != nil {
		t.Fatalf("the verified first frame must still read: %v", err)
	}
	r := f.reader(FrameSize, bytes.NewReader(body[len(f.frames[0]):]))
	if _, err := io.ReadAll(r); !errors.Is(err, ErrFrame) {
		t.Fatalf("err %v, want ErrFrame", err)
	}
	if _, err := r.Read(make([]byte, 1)); !errors.Is(err, ErrFrame) {
		t.Fatalf("the error is not sticky: %v", err)
	}
}

func TestReaderReportsACutFrameAsUnexpectedEOF(t *testing.T) {
	t.Parallel()
	f := newReaderFixture(t, 2*FrameSize)
	body := concat(f.frames)
	cases := []struct {
		name string
		body []byte
		want error
	}{
		{"cut inside the second frame", body[:len(f.frames[0])+5000], io.ErrUnexpectedEOF},
		{"cut inside the second length prefix", body[:len(f.frames[0])+2], io.ErrUnexpectedEOF},
		{"cut one byte before the end", body[:len(body)-1], io.ErrUnexpectedEOF},
		{"cut exactly at the frame boundary", body[:len(f.frames[0])], nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			_, err := io.Copy(&out, f.reader(0, bytes.NewReader(tc.body)))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err %v, want %v", err, tc.want)
			}
			if !bytes.Equal(out.Bytes(), f.plain[:FrameSize]) {
				t.Fatal("the verified first frame was not delivered intact")
			}
		})
	}
}

func TestReaderResumesAtAnyOffset(t *testing.T) {
	t.Parallel()
	f := newReaderFixture(t, 2*FrameSize+FrameSize/2)
	cases := []struct {
		name   string
		offset int64
	}{
		{"at a frame boundary", FrameSize},
		{"inside a frame after a partial disk write", FrameSize + 12345},
		{"inside the last frame", 2*FrameSize + 99},
		{"at the end", int64(len(f.plain))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first := int(tc.offset / FrameSize)
			got := readAll(t, f.reader(tc.offset, bytes.NewReader(concat(f.frames[min(first, len(f.frames)):]))))
			if !bytes.Equal(got, f.plain[tc.offset:]) {
				t.Fatalf("resume at %d yielded %d bytes, want %d", tc.offset, len(got), len(f.plain)-int(tc.offset))
			}
		})
	}
	beyond := f.reader(2*FrameSize+FrameSize/2+1, bytes.NewReader(f.frames[2]))
	if _, err := io.ReadAll(beyond); !errors.Is(err, ErrFrame) {
		t.Fatalf("offset beyond the last frame: err %v, want ErrFrame", err)
	}
}

func TestWriteToWritesExactlyOneFramePerCall(t *testing.T) {
	t.Parallel()
	f := newReaderFixture(t, 2*FrameSize+FrameSize/2)
	var out writeRecorder
	n, err := writeTo(t, f.reader(0, bytes.NewReader(concat(f.frames))), &out)
	if err != nil || n != int64(len(f.plain)) {
		t.Fatalf("wrote %d, err %v", n, err)
	}
	want := []int{FrameSize, FrameSize, FrameSize / 2}
	if len(out.sizes) != len(want) {
		t.Fatalf("writes %v, want %v", out.sizes, want)
	}
	for i := range want {
		if out.sizes[i] != want[i] {
			t.Fatalf("writes %v, want %v", out.sizes, want)
		}
	}
}

func TestReaderHandlesAZeroLengthUpload(t *testing.T) {
	t.Parallel()
	f := newReaderFixture(t, 0)
	if len(f.frames) != 0 {
		t.Fatalf("%d frames for an empty file", len(f.frames))
	}
	n, err := f.reader(0, bytes.NewReader(nil)).Read(make([]byte, 16))
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("read %d, err %v, want 0 and EOF", n, err)
	}
	written, err := writeTo(t, f.reader(0, bytes.NewReader(nil)), io.Discard)
	if written != 0 || err != nil {
		t.Fatalf("wrote %d, err %v", written, err)
	}
}

func TestReaderRejectsBadLengthsBeforeReadingTheBody(t *testing.T) {
	t.Parallel()
	f := newReaderFixture(t, FrameSize)
	cases := []struct {
		name   string
		header []byte
	}{
		{"zero length", []byte{0, 0, 0, 0}},
		{"length above FrameSize", []byte{0, 0x10, 0, 1}},
		{"maximal length", []byte{0xff, 0xff, 0xff, 0xff}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := io.ReadAll(f.reader(0, bytes.NewReader(tc.header)))
			if !errors.Is(err, ErrFrame) {
				t.Fatalf("err %v, want ErrFrame", err)
			}
		})
	}
}

func TestReaderPassesTheBodyErrorThrough(t *testing.T) {
	t.Parallel()
	f := newReaderFixture(t, FrameSize)
	errBody := errors.New("connection reset")
	body := io.MultiReader(bytes.NewReader(f.frames[0][:1000]), failingReader{errBody})
	if _, err := io.ReadAll(f.reader(0, body)); !errors.Is(err, errBody) {
		t.Fatalf("err %v, want the body's error", err)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestNewReaderRejectsABadNonceOrOffset(t *testing.T) {
	t.Parallel()
	f := newReaderFixture(t, FrameSize)
	short := NewReader(&f.key, f.nonce[:UploadNonceSize-1], 0, bytes.NewReader(f.frames[0]))
	if _, err := io.ReadAll(short); !errors.Is(err, ErrFrame) {
		t.Fatalf("short nonce: err %v, want ErrFrame", err)
	}
	if _, err := io.ReadAll(f.reader(-1, bytes.NewReader(f.frames[0]))); !errors.Is(err, ErrFrame) {
		t.Fatalf("negative offset: err %v, want ErrFrame", err)
	}
}

func TestReadWorksWithSmallBuffersAndMixesWithWriteTo(t *testing.T) {
	t.Parallel()
	f := newReaderFixture(t, 2*FrameSize+FrameSize/2)
	r := f.reader(0, bytes.NewReader(concat(f.frames)))
	head := make([]byte, 1000)
	if _, err := io.ReadFull(r, head); err != nil {
		t.Fatal(err)
	}
	var tail bytes.Buffer
	if _, err := writeTo(t, r, &tail); err != nil {
		t.Fatal(err)
	}
	if got := slices.Concat(head, tail.Bytes()); !bytes.Equal(got, f.plain) {
		t.Fatal("plaintext differs when Read and WriteTo are mixed")
	}
}

func writeTo(t *testing.T, r io.Reader, w io.Writer) (int64, error) {
	t.Helper()
	wt, ok := r.(io.WriterTo)
	if !ok {
		t.Fatal("the frame reader does not implement io.WriterTo")
	}
	return wt.WriteTo(w)
}
