package inbox

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/tus/tusd/v2/pkg/handler"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/store"
)

const (
	tusVersion           = "1.0.0"
	methodOverrideHeader = "X-Http-Method-Override"
)

func (in *Inbox) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Del(methodOverrideHeader)
		id := strings.Trim(r.URL.Path, "/")
		if id == "" && r.Method != http.MethodPost {
			in.tus.ServeHTTP(w, r)
			return
		}
		deviceID, ok := in.cfg.DeviceID(r.Context())
		if !ok {
			in.log.Error("upload request without device", "method", r.Method, "transfer", id)
			in.writeError(w, r, http.StatusInternalServerError, api.CodeInternal, "request has no device")
			return
		}
		if id == "" {
			in.serveCreate(w, r, deviceID)
			return
		}
		if !in.isOwnedBy(id, deviceID) {
			in.serveFinished(w, r, id, deviceID)
			return
		}
		switch r.Method {
		case http.MethodHead:
			in.serveResume(w, r, id, deviceID)
		case http.MethodPatch:
			if !in.beginWrite(deviceID) {
				in.rejectBusy(w, r, deviceID)
				return
			}
			defer in.endWrite(deviceID)
			in.serveResume(w, r, id, deviceID)
		default:
			in.tus.ServeHTTP(w, r)
		}
	})
}

func (in *Inbox) serveResume(w http.ResponseWriter, r *http.Request, id, deviceID string) {
	isLive, err := in.finishIfComplete(r.Context(), id)
	if err != nil {
		in.log.Error("completed upload not moved into received dir", "transfer", id, "err", err)
		in.writeError(w, r, http.StatusLocked, api.CodeInternal, "upload not finished, retry")
		return
	}
	if !isLive {
		in.serveFinished(w, r, id, deviceID)
		return
	}
	if r.Method == http.MethodPatch {
		if !in.admitWrite(w, r, id, deviceID) {
			return
		}
		sealed, body, ok := in.sealPatch(w, r, id, deviceID)
		if !ok {
			return
		}
		defer body.stop()
		r = sealed
	}
	in.tus.ServeHTTP(w, r)
}

func (in *Inbox) sealPatch(w http.ResponseWriter, r *http.Request, id, deviceID string) (*http.Request, *sealedBody, bool) {
	session, ok := in.sessionOf(r.Context(), deviceID)
	if !ok {
		in.writeError(w, r, http.StatusForbidden, api.CodeSealExpired, "sealed session required")
		return nil, nil, false
	}
	info, _, err := in.liveInfo(id)
	if err != nil && !errors.Is(err, handler.ErrNotFound) {
		in.log.Error("upload info not read", "transfer", id, "err", err)
		in.writeError(w, r, http.StatusInternalServerError, api.CodeInternal, "upload info not read")
		return nil, nil, false
	}
	offset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil {
		offset = -1
	}
	sealed, body := in.sealedRequest(r, session, uploadNonce(info.MetaData), offset, &uploadRef{id: id})
	return sealed, body, true
}

func (in *Inbox) serveCreate(w http.ResponseWriter, r *http.Request, deviceID string) {
	created := &uploadRef{}
	ctx := context.WithValue(r.Context(), uploadRefKey{}, created)
	create := r.WithContext(ctx)
	if r.Header.Get("Content-Type") == tusContentType {
		if !in.beginWrite(deviceID) {
			in.rejectBusy(w, r, deviceID)
			return
		}
		defer in.endWrite(deviceID)
		session, ok := in.sessionOf(ctx, deviceID)
		if !ok {
			in.writeError(w, r, http.StatusForbidden, api.CodeSealExpired, "sealed session required")
			return
		}
		nonce := uploadNonce(handler.ParseMetadataHeader(r.Header.Get("Upload-Metadata")))
		sealed, body := in.sealedRequest(create, session, nonce, 0, created)
		defer body.stop()
		create = sealed
	} else if in.writesOf(deviceID) >= in.cfg.MaxActivePerDevice {
		in.rejectBusy(w, r, deviceID)
		return
	}
	recorder := &statusRecorder{ResponseWriter: w}
	in.tus.ServeHTTP(recorder, create)
	switch {
	case created.id == "":
	case recorder.status == http.StatusCreated:
		in.announce(created.id)
	default:
		in.abandon(ctx, created.id)
	}
}

func (in *Inbox) abandon(ctx context.Context, transferID string) {
	t, ok := in.tracked(transferID)
	if !ok {
		return
	}
	ctx = context.WithoutCancel(ctx)
	if err := in.terminate(ctx, transferID); err != nil {
		in.log.Error("upload of an unanswered creation request not removed", "transfer", transferID, "err", err)
	}
	in.untrack(transferID)
	if err := in.store.DeleteTransfer(ctx, transferID); err != nil {
		in.log.Error("transfer of an unanswered creation request not deleted", "transfer", transferID, "err", err)
		return
	}
	in.log.Info("upload creation not answered, transfer removed", "transfer", transferID, "device", t.DeviceID, "bytes", t.Done)
}

func (in *Inbox) rejectBusy(w http.ResponseWriter, r *http.Request, deviceID string) {
	in.log.Warn("upload rejected, device write limit reached", "device", deviceID, "method", r.Method)
	in.writeError(w, r, http.StatusTooManyRequests, api.CodeRateLimited, "too many concurrent uploads")
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	if s.status == 0 {
		s.status = status
	}
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(p []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(p)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

func (in *Inbox) admitWrite(w http.ResponseWriter, r *http.Request, id, deviceID string) bool {
	hasSpace, err := in.hasSpaceFor(0)
	if err != nil {
		in.log.Error("free space unknown", "path", in.cfg.ReceivedDir, "err", err)
		in.writeError(w, r, http.StatusInternalServerError, api.CodeInternal, "free space unknown")
		return false
	}
	if !hasSpace {
		in.log.Warn("upload paused, not enough free space", "device", deviceID, "transfer", id)
		in.writeError(w, r, http.StatusInsufficientStorage, api.CodeNoSpace, "not enough free space")
		return false
	}
	return true
}

func (in *Inbox) serveFinished(w http.ResponseWriter, r *http.Request, id, deviceID string) {
	if r.Method == http.MethodHead || r.Method == http.MethodPatch {
		t, err := in.store.Transfer(r.Context(), id)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			in.log.Error("transfer not read", "transfer", id, "err", err)
			in.writeError(w, r, http.StatusInternalServerError, api.CodeInternal, "transfer not read")
			return
		}
		if err == nil && t.DeviceID == deviceID && t.Direction == store.DirectionIn && t.Status == store.TransferDone {
			writeFinished(w, r, t.Size)
			return
		}
	}
	in.writeError(w, r, http.StatusNotFound, api.CodeNotFound, "upload not found")
}

func writeFinished(w http.ResponseWriter, r *http.Request, size int64) {
	header := w.Header()
	header.Set("Tus-Resumable", tusVersion)
	header.Set("Cache-Control", "no-store")
	header.Set("Upload-Offset", strconv.FormatInt(size, 10))
	header.Set("Upload-Length", strconv.FormatInt(size, 10))
	if r.Method == http.MethodPatch {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (in *Inbox) writeError(w http.ResponseWriter, r *http.Request, status int, code api.ErrorCode, message string) {
	w.Header().Set("Content-Type", jsonContentType)
	if status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", retryAfterSeconds)
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := io.WriteString(w, in.errorBody(code, message)); err != nil {
		in.log.Debug("error response not written", "err", err)
	}
}
