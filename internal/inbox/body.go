package inbox

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/tus/tusd/v2/pkg/handler"

	"github.com/1etu/ferry/internal/seal"
)

var errFrameRejected = handler.NewError(handler.ErrUploadInterrupted.ErrorCode, "sealed frame rejected", http.StatusBadRequest)

type sessionKey struct{}

func ContextWithSession(ctx context.Context, s seal.Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

func SessionFromContext(ctx context.Context) (seal.Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(seal.Session)
	return s, ok
}

type uploadRefKey struct{}

type uploadRef struct {
	id string
}

func createdUpload(ctx context.Context) (*uploadRef, bool) {
	ref, ok := ctx.Value(uploadRefKey{}).(*uploadRef)
	return ref, ok
}

func (in *Inbox) sessionOf(ctx context.Context, deviceID string) (seal.Session, bool) {
	s, ok := in.cfg.Session(ctx)
	return s, ok && s.DeviceID == deviceID
}

func uploadNonce(metadata handler.MetaData) []byte {
	nonce, err := base64.RawURLEncoding.DecodeString(metadata[metadataNonce])
	if err != nil || len(nonce) != seal.UploadNonceSize {
		return nil
	}
	return nonce
}

func (in *Inbox) sealedRequest(r *http.Request, s seal.Session, nonce []byte, offset int64, upload *uploadRef) (*http.Request, *sealedBody) {
	ring := newReadAhead(r.Body, &in.rings)
	body := &sealedBody{
		frames:   seal.NewReader(&s.Key, nonce, offset, ring),
		ring:     ring,
		original: r.Body,
		upload:   upload,
		deviceID: s.DeviceID,
		log:      in.log,
	}
	sealed := r.WithContext(r.Context())
	sealed.Body = body
	sealed.ContentLength = -1
	return sealed, body
}

type sealedBody struct {
	frames     io.Reader
	ring       *readAhead
	original   io.ReadCloser
	upload     *uploadRef
	deviceID   string
	log        *slog.Logger
	isRejected bool
}

func (b *sealedBody) Read(p []byte) (int, error) {
	n, err := b.frames.Read(p)
	if !errors.Is(err, seal.ErrFrame) {
		return n, err
	}
	b.ring.stop()
	if !b.isRejected {
		b.isRejected = true
		b.log.Warn("sealed frame rejected", "device", b.deviceID, "transfer", b.upload.id)
	}
	return n, errFrameRejected
}

func (b *sealedBody) stop() {
	b.ring.stop()
}

func (b *sealedBody) Close() error {
	b.ring.stop()
	return b.original.Close()
}
