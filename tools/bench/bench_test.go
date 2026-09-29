package main

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/1etu/ferry/internal/seal"
)

func TestSealRangeOpensUnderTheServerReader(t *testing.T) {
	t.Parallel()
	var key [32]byte
	nonce := randomBytes(seal.UploadNonceSize)
	plain := pattern(2*seal.FrameSize + 12345)
	tests := []struct {
		name  string
		start int64
	}{
		{"first chunk", 0},
		{"later chunk", 16 * mebibyte},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sealed := sealRange(&key, nonce, plain, tt.start)
			opened, err := io.ReadAll(seal.NewReader(&key, nonce, tt.start, bytes.NewReader(sealed)))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(opened, plain) {
				t.Fatalf("opened %d bytes that differ from the %d sealed", len(opened), len(plain))
			}
		})
	}
}

func TestParseOptionsRejectsUnframedSizes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"defaults", nil, false},
		{"chunk not a frame multiple", []string{"-chunk", "1000"}, true},
		{"burst file larger than the chunk", []string{"-chunk", "1048576", "-file-size", "2000000"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseOptions(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err %v, want error %v", err, tt.wantErr)
			}
		})
	}
}

func TestPercentileReadsTheSortedRank(t *testing.T) {
	t.Parallel()
	sorted := []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond, 5 * time.Millisecond}
	if got := percentile(sorted, 50); got != 3*time.Millisecond {
		t.Fatalf("p50 %s, want 3ms", got)
	}
	if got := percentile(sorted, 95); got != 4*time.Millisecond {
		t.Fatalf("p95 %s, want 4ms", got)
	}
}
