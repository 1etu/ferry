package outbox

import (
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

const (
	progressChunkBytes = 1 << 20
	copyBufferBytes    = 256 << 10
	hasNoSendfile      = runtime.GOOS == "windows"
)

var copyBuffers sync.Pool

type trackingWriter struct {
	http.ResponseWriter
	cover       func(start, end int64)
	offset      int64
	wroteHeader bool
	isCounting  bool
	isPooled    bool
}

func (tw *trackingWriter) WriteHeader(status int) {
	if !tw.wroteHeader {
		tw.wroteHeader = true
		tw.offset, tw.isCounting = bodyStart(status, tw.Header())
	}
	tw.ResponseWriter.WriteHeader(status)
}

func (tw *trackingWriter) Write(p []byte) (int, error) {
	if !tw.wroteHeader {
		tw.WriteHeader(http.StatusOK)
	}
	n, err := tw.ResponseWriter.Write(p)
	tw.count(int64(n))
	return n, err
}

func (tw *trackingWriter) ReadFrom(src io.Reader) (int64, error) {
	if !tw.wroteHeader {
		tw.WriteHeader(http.StatusOK)
	}
	if tw.isPooled {
		return copyPooled(tw, src)
	}
	if !tw.isCounting {
		return io.Copy(tw.ResponseWriter, src)
	}
	limited, ok := src.(*io.LimitedReader)
	if !ok {
		return io.Copy(struct{ io.Writer }{tw}, src)
	}
	var total int64
	for limited.N > 0 {
		chunk := &io.LimitedReader{R: limited.R, N: min(limited.N, progressChunkBytes)}
		n, err := io.Copy(tw.ResponseWriter, chunk)
		limited.N -= n
		total += n
		tw.count(n)
		if err != nil || n == 0 {
			return total, err
		}
	}
	return total, nil
}

func (tw *trackingWriter) count(n int64) {
	if !tw.isCounting || n == 0 {
		return
	}
	start := tw.offset
	tw.offset += n
	tw.cover(start, tw.offset)
}

func bodyStart(status int, h http.Header) (int64, bool) {
	switch status {
	case http.StatusOK:
		return 0, true
	case http.StatusPartialContent:
		return rangeStart(h.Get("Content-Range"))
	default:
		return 0, false
	}
}

func rangeStart(contentRange string) (int64, bool) {
	spec, ok := strings.CutPrefix(contentRange, "bytes ")
	if !ok {
		return 0, false
	}
	first, _, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, false
	}
	start, err := strconv.ParseInt(first, 10, 64)
	return start, err == nil
}

type pooledWriter struct {
	http.ResponseWriter
}

func downloadWriter(w http.ResponseWriter) http.ResponseWriter {
	if hasNoSendfile {
		return &pooledWriter{ResponseWriter: w}
	}
	return w
}

func (pw *pooledWriter) ReadFrom(src io.Reader) (int64, error) {
	return copyPooled(pw.ResponseWriter, src)
}

func (pw *pooledWriter) Unwrap() http.ResponseWriter {
	return pw.ResponseWriter
}

type writerOnly struct {
	io.Writer
}

type readerOnly struct {
	io.Reader
}

func copyPooled(dst io.Writer, src io.Reader) (int64, error) {
	buffer, ok := copyBuffers.Get().(*[]byte)
	if !ok {
		allocated := make([]byte, copyBufferBytes)
		buffer = &allocated
	}
	defer copyBuffers.Put(buffer)
	return io.CopyBuffer(writerOnly{dst}, readerOnly{src}, *buffer)
}
