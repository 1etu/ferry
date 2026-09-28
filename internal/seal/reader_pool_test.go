package seal

import (
	"bytes"
	"errors"
	"io"
	"runtime"
	"runtime/debug"
	"testing"
)

func TestReaderTakesItsFrameBufferOnTheFirstReadAndReturnsItAtTheEnd(t *testing.T) {
	t.Parallel()
	f := newReaderFixture(t, 2*FrameSize)
	tampered := concat(f.frames)
	tampered[len(tampered)-1] ^= 0x01
	cases := []struct {
		name string
		body []byte
		want error
	}{
		{"at EOF", concat(f.frames), nil},
		{"after a tampered frame", tampered, ErrFrame},
		{"after a cut frame", concat(f.frames)[:len(f.frames[0])+10], io.ErrUnexpectedEOF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := f.reader(0, bytes.NewReader(tc.body))
			fr, ok := r.(*frameReader)
			if !ok {
				t.Fatalf("reader is %T", r)
			}
			if fr.frame != nil {
				t.Fatal("frame buffer taken before the first read")
			}
			if _, err := io.Copy(io.Discard, r); !errors.Is(err, tc.want) {
				t.Fatalf("err %v, want %v", err, tc.want)
			}
			if fr.frame != nil {
				t.Fatal("frame buffer not returned at the end")
			}
		})
	}
}

func TestReaderReusesFrameBuffersAcrossReaders(t *testing.T) {
	f := newReaderFixture(t, 2*FrameSize)
	body := concat(f.frames)
	drain := func() {
		t.Helper()
		if _, err := io.Copy(io.Discard, f.reader(0, bytes.NewReader(body))); err != nil {
			t.Fatal(err)
		}
	}
	drain()
	const readers = 32
	previous := debug.SetGCPercent(-1)
	t.Cleanup(func() { debug.SetGCPercent(previous) })
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range readers {
		drain()
	}
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > readers/4*frameBufferSize {
		t.Fatalf("%d readers allocated %d bytes, want pooled frame buffers", readers, allocated)
	}
}
