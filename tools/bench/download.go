package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/auth"
)

const readBufferBytes = 1 << 20

func runDownload(ctx context.Context, s *server, d *device, o options) (result, error) {
	plain := pattern(o.chunk)
	source := filepath.Join(s.scratch, "bench-download.bin")
	if err := writeSource(source, plain, o.size); err != nil {
		return result{}, err
	}
	var offered []api.OfferedFile
	if err := s.owner(ctx, http.MethodPost, "/api/files", api.OfferRequest{Paths: []string{source}}, &offered); err != nil {
		return result{}, err
	}
	if len(offered) != 1 {
		return result{}, fmt.Errorf("offer returned %d files, want 1", len(offered))
	}
	time.Sleep(o.settle)
	start := time.Now()
	received, err := d.download(ctx, offered[0].ID)
	elapsed := time.Since(start)
	if err != nil {
		return result{}, err
	}
	if received != o.size {
		return result{}, fmt.Errorf("downloaded %d bytes, want %d", received, o.size)
	}
	return result{elapsed: elapsed, bytes: received}, nil
}

func (d *device) download(ctx context.Context, fileID string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.server.url+"/api/files/"+fileID+"/content", http.NoBody)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Cookie", auth.CookieName+"="+d.cookie)
	req.Header.Set("X-Forwarded-For", forwardedFor)
	resp, err := d.server.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download %s: %w", fileID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, unexpected(resp, "download "+fileID)
	}
	buffer := make([]byte, readBufferBytes)
	var total int64
	for {
		n, err := resp.Body.Read(buffer)
		total += int64(n)
		if errors.Is(err, io.EOF) {
			return total, nil
		}
		if err != nil {
			return total, fmt.Errorf("download %s after %d bytes: %w", fileID, total, err)
		}
	}
}

func writeSource(path string, plain []byte, size int64) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create source: %w", err)
	}
	for written := int64(0); written < size; written += int64(len(plain)) {
		if _, err := f.Write(plain[:min(int64(len(plain)), size-written)]); err != nil {
			return errors.Join(fmt.Errorf("write source: %w", err), f.Close())
		}
	}
	return f.Close()
}

func verifyReceived(path string, plain []byte, size int64) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open received file: %w", err)
	}
	defer f.Close()
	got := sha256.New()
	if _, err := io.Copy(got, bufio.NewReaderSize(f, readBufferBytes)); err != nil {
		return fmt.Errorf("hash received file: %w", err)
	}
	want := sha256.New()
	for written := int64(0); written < size; written += int64(len(plain)) {
		want.Write(plain[:min(int64(len(plain)), size-written)])
	}
	if !bytes.Equal(got.Sum(nil), want.Sum(nil)) {
		return fmt.Errorf("received file %s differs from the source", path)
	}
	return nil
}
