package inbox

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/tus/tusd/v2/pkg/handler"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/seal"
	"github.com/1etu/ferry/internal/store"
)

const (
	metadataName         = "name"
	metadataNonce        = "nonce"
	metadataFiletype     = "filetype"
	metadataLastModified = "lastModified"
)

var errNameNotSealed = errors.New("name metadata is missing, blank or not sealed under the session")

func (in *Inbox) preCreate(hook handler.HookEvent) (handler.HTTPResponse, handler.FileInfoChanges, error) {
	deviceID, ok := in.cfg.DeviceID(hook.Context)
	if !ok {
		in.log.Error("upload creation without device")
		return in.rejected(http.StatusInternalServerError, api.CodeInternal, "request has no device")
	}
	if hook.Upload.SizeIsDeferred {
		return in.rejected(http.StatusBadRequest, api.CodeInvalidRequest, "Upload-Defer-Length is not supported")
	}
	session, ok := in.sessionOf(hook.Context, deviceID)
	if !ok {
		return in.rejected(http.StatusForbidden, api.CodeSealExpired, "sealed session required")
	}
	name, err := openName(session, hook.Upload.MetaData)
	if err != nil {
		in.log.Warn("upload rejected, name not sealed", "device", deviceID, "err", err)
		return in.rejected(http.StatusBadRequest, api.CodeInvalidRequest, "name metadata must be sealed")
	}
	if uploadNonce(hook.Upload.MetaData) == nil {
		return in.rejected(http.StatusBadRequest, api.CodeInvalidRequest, "nonce metadata is required")
	}
	size := hook.Upload.Size
	if size > in.cfg.MaxUploadBytes {
		in.log.Warn("upload rejected, too large", "device", deviceID, "bytes", size)
		return in.rejected(http.StatusRequestEntityTooLarge, api.CodeTooLarge, "upload exceeds the size limit")
	}
	hasSpace, err := in.hasSpaceFor(size)
	if err != nil {
		in.log.Error("free space unknown", "path", in.cfg.ReceivedDir, "err", err)
		return in.rejected(http.StatusInternalServerError, api.CodeInternal, "free space unknown")
	}
	if !hasSpace {
		in.log.Warn("upload rejected, not enough free space", "device", deviceID, "bytes", size)
		return in.rejected(http.StatusInsufficientStorage, api.CodeNoSpace, "not enough free space")
	}
	now := time.Now()
	t := store.Transfer{
		ID:        ulid.Make().String(),
		DeviceID:  deviceID,
		Direction: store.DirectionIn,
		Name:      SanitizeFilename(name),
		Size:      size,
		Status:    store.TransferActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := in.store.InsertTransfer(hook.Context, t); err != nil {
		in.log.Error("upload not recorded", "device", deviceID, "err", err)
		return in.rejected(http.StatusInternalServerError, api.CodeInternal, "upload not recorded")
	}
	in.trackCreation(t)
	if created, ok := createdUpload(hook.Context); ok {
		created.id = t.ID
	}
	in.log.Info("upload created", "transfer", t.ID, "device", deviceID, "bytes", size)
	return handler.HTTPResponse{}, handler.FileInfoChanges{ID: t.ID, MetaData: keptMetadata(hook.Upload.MetaData, t.Name)}, nil
}

func (in *Inbox) preFinish(hook handler.HookEvent) (handler.HTTPResponse, error) {
	id := hook.Upload.ID
	t, ok := in.tracked(id)
	if !ok {
		in.log.Error("finished upload is not tracked", "transfer", id)
		return handler.HTTPResponse{}, in.hookError(http.StatusInternalServerError, api.CodeInternal, "upload not tracked")
	}
	t.Done = t.Size
	if err := in.complete(hook.Context, t); err != nil {
		in.log.Error("completed upload not moved into received dir", "transfer", id, "err", err)
		return handler.HTTPResponse{}, in.hookError(http.StatusLocked, api.CodeInternal, "upload not finished, retry")
	}
	return handler.HTTPResponse{}, nil
}

func (in *Inbox) preTerminate(hook handler.HookEvent) (handler.HTTPResponse, error) {
	if err := in.markCanceled(hook.Context, hook.Upload.ID); err != nil {
		in.log.Error("canceled upload not recorded", "transfer", hook.Upload.ID, "err", err)
		return handler.HTTPResponse{}, in.hookError(http.StatusInternalServerError, api.CodeInternal, "cancellation not recorded")
	}
	return handler.HTTPResponse{}, nil
}

func openName(s seal.Session, metadata handler.MetaData) (string, error) {
	name, err := seal.OpenString(&s.Key, metadata[metadataName])
	if err != nil {
		return "", errors.Join(errNameNotSealed, err)
	}
	if strings.TrimSpace(name) == "" {
		return "", errNameNotSealed
	}
	return name, nil
}

func keptMetadata(received handler.MetaData, sanitizedName string) handler.MetaData {
	kept := handler.MetaData{metadataName: sanitizedName, metadataNonce: received[metadataNonce]}
	for _, key := range []string{metadataFiletype, metadataLastModified} {
		if value, ok := received[key]; ok {
			kept[key] = value
		}
	}
	return kept
}

func (in *Inbox) rejected(status int, code api.ErrorCode, message string) (handler.HTTPResponse, handler.FileInfoChanges, error) {
	return handler.HTTPResponse{}, handler.FileInfoChanges{}, in.hookError(status, code, message)
}

func (in *Inbox) hookError(status int, code api.ErrorCode, message string) handler.Error {
	header := handler.HTTPHeader{"Content-Type": jsonContentType}
	if status == http.StatusTooManyRequests {
		header["Retry-After"] = retryAfterSeconds
	}
	return handler.Error{
		ErrorCode:    string(code),
		Message:      message,
		HTTPResponse: handler.HTTPResponse{StatusCode: status, Body: in.errorBody(code, message), Header: header},
	}
}

func (in *Inbox) errorBody(code api.ErrorCode, message string) string {
	encoded, err := json.Marshal(api.NewError(code, message))
	if err != nil {
		in.log.Error("error response not encoded", "code", string(code), "err", err)
	}
	return string(encoded)
}
