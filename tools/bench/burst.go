package main

import (
	"context"
	"crypto/rand"
	"fmt"
	mathrand "math/rand/v2"
	"runtime"
	"sync"
	"time"

	"github.com/1etu/ferry/internal/seal"
)

const frameOverhead = 4 + 16

type sealedChunk struct {
	start int64
	end   int64
	body  []byte
}

type burstFile struct {
	name  string
	nonce []byte
	chunk sealedChunk
}

func runBurst(ctx context.Context, _ *server, d *device, o options) (result, error) {
	files, err := prepareBurst(&d.key, o.files, pattern(o.fileSize))
	if err != nil {
		return result{}, err
	}
	time.Sleep(o.settle)
	latencies := make([]time.Duration, len(files))
	next := make(chan int)
	errs := make(chan error, o.parallel)
	var wg sync.WaitGroup
	start := time.Now()
	for range o.parallel {
		wg.Go(func() {
			for i := range next {
				began := time.Now()
				f := files[i]
				if _, err := d.create(ctx, f.name, f.chunk.end, f.nonce, f.chunk); err != nil {
					errs <- fmt.Errorf("file %d: %w", i, err)
					return
				}
				latencies[i] = time.Since(began)
			}
		})
	}
	err = feed(next, len(files), errs)
	wg.Wait()
	elapsed := time.Since(start)
	if err != nil {
		return result{}, err
	}
	return result{elapsed: elapsed, bytes: int64(len(files)) * o.fileSize, files: len(files), latencies: latencies}, nil
}

func feed(next chan<- int, count int, errs <-chan error) error {
	defer close(next)
	for i := range count {
		select {
		case next <- i:
		case err := <-errs:
			return err
		}
	}
	return nil
}

func prepareBurst(key *[32]byte, count int, plain []byte) ([]burstFile, error) {
	files := make([]burstFile, count)
	indices := make(chan int)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Go(func() {
			for i := range indices {
				nonce := randomBytes(seal.UploadNonceSize)
				files[i] = burstFile{
					name:  fmt.Sprintf("IMG_%04d.JPG", i),
					nonce: nonce,
					chunk: sealedChunk{end: int64(len(plain)), body: sealRange(key, nonce, plain, 0)},
				}
			}
		})
	}
	for i := range count {
		indices <- i
	}
	close(indices)
	wg.Wait()
	for i := range files {
		if files[i].chunk.body == nil {
			return nil, fmt.Errorf("file %d not sealed", i)
		}
	}
	return files, nil
}

func sealAhead(ctx context.Context, key *[32]byte, nonce, plain []byte, size, chunk int64) <-chan chan sealedChunk {
	futures := make(chan chan sealedChunk, sealAheadDepth)
	go func() {
		defer close(futures)
		for start := int64(0); start < size; start += chunk {
			end := min(start+chunk, size)
			future := make(chan sealedChunk, 1)
			select {
			case futures <- future:
			case <-ctx.Done():
				return
			}
			go func() {
				future <- sealedChunk{start: start, end: end, body: sealRange(key, nonce, plain[:end-start], start)}
			}()
		}
	}()
	return futures
}

func sealRange(key *[32]byte, nonce, plain []byte, start int64) []byte {
	frames := (len(plain) + seal.FrameSize - 1) / seal.FrameSize
	body := make([]byte, 0, len(plain)+frames*frameOverhead)
	for offset := 0; offset < len(plain); offset += seal.FrameSize {
		frame, err := seal.SealFrame(key, nonce, frameIndex(start+int64(offset)), plain[offset:min(offset+seal.FrameSize, len(plain))])
		if err != nil {
			panic(err)
		}
		body = append(body, frame...)
	}
	return body
}

func frameIndex(offset int64) uint64 {
	return uint64(max(offset, 0)) / seal.FrameSize
}

func pattern(size int64) []byte {
	plain := make([]byte, size)
	source := mathrand.NewChaCha8([32]byte{'f', 'e', 'r', 'r', 'y'})
	if _, err := source.Read(plain); err != nil {
		panic(err)
	}
	return plain
}

func randomBytes(size int) []byte {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
