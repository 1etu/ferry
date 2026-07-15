package api

import (
	"encoding/base64"
	"net/url"
	"time"

	"github.com/1etu/ferry/internal/store"
)

const (
	AppName         = "ferry"
	timestampLayout = "2006-01-02T15:04:05.000Z07:00"
)

type ErrorCode string

const (
	CodeInvalidRequest  ErrorCode = "invalid_request"
	CodeUnauthorized    ErrorCode = "unauthorized"
	CodeForbidden       ErrorCode = "forbidden"
	CodePendingApproval ErrorCode = "pending_approval"
	CodeNotFound        ErrorCode = "not_found"
	CodePairingInvalid  ErrorCode = "pairing_invalid"
	CodePairingExpired  ErrorCode = "pairing_expired"
	CodeRateLimited     ErrorCode = "rate_limited"
	CodeTooLarge        ErrorCode = "too_large"
	CodeNoSpace         ErrorCode = "no_space"
	CodeFileMissing     ErrorCode = "file_missing"
	CodeExpired         ErrorCode = "expired"
	CodeConflict        ErrorCode = "conflict"
	CodeUnsupported     ErrorCode = "unsupported"
	CodeSealExpired     ErrorCode = "seal_expired"
	CodeSealInvalid     ErrorCode = "seal_invalid"
	CodeInternal        ErrorCode = "internal"
)

func ErrorCodes() []ErrorCode {
	return []ErrorCode{
		CodeInvalidRequest, CodeUnauthorized, CodeForbidden, CodePendingApproval, CodeNotFound,
		CodePairingInvalid, CodePairingExpired, CodeRateLimited, CodeTooLarge, CodeNoSpace,
		CodeFileMissing, CodeExpired, CodeConflict, CodeUnsupported, CodeSealExpired, CodeSealInvalid,
		CodeInternal,
	}
}

type Error struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func NewError(code ErrorCode, message string) Error {
	return Error{Error: ErrorDetail{Code: code, Message: message}}
}

type Role string

const (
	RoleNone   Role = "none"
	RoleDevice Role = "device"
	RoleOwner  Role = "owner"
)

type Health struct {
	App     string `json:"app"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Session struct {
	Role   Role       `json:"role"`
	Device *Device    `json:"device,omitempty"`
	Server ServerInfo `json:"server"`
}

type ServerInfo struct {
	Name    string  `json:"name"`
	Version string  `json:"version"`
	Origins Origins `json:"origins"`
}

type Origins struct {
	Local string `json:"local"`
	IP    string `json:"ip"`
}

type PairRequest struct {
	Token     string `json:"token"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	HasSecret bool   `json:"hasSecret"`
}

type Pairing struct {
	QRURL     string `json:"qrUrl"`
	LocalURL  string `json:"localUrl"`
	Code      string `json:"code"`
	ExpiresAt string `json:"expiresAt"`
}

func PairingURL(origin, token string, secret []byte) string {
	return origin + "/?pair=" + url.QueryEscape(token) + "#s=" + base64.RawURLEncoding.EncodeToString(secret)
}

type Device struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Status     store.DeviceStatus `json:"status"`
	CreatedAt  string             `json:"createdAt"`
	ApprovedAt string             `json:"approvedAt,omitempty"`
	LastSeenAt string             `json:"lastSeenAt,omitempty"`
}

func DeviceFrom(d store.Device) Device {
	return Device{
		ID:         d.ID,
		Name:       d.Name,
		Status:     d.Status,
		CreatedAt:  Timestamp(d.CreatedAt),
		ApprovedAt: OptionalTimestamp(d.ApprovedAt),
		LastSeenAt: OptionalTimestamp(d.LastSeenAt),
	}
}

const (
	DeviceRequested = "requested"
	DeviceApproved  = "approved"
	DeviceRevoked   = "revoked"
)

type DeviceChange struct {
	Action string `json:"action"`
	Device Device `json:"device"`
}

type Transfer struct {
	ID        string               `json:"id"`
	DeviceID  string               `json:"deviceId"`
	Direction store.Direction      `json:"direction"`
	Name      string               `json:"name"`
	Size      int64                `json:"size"`
	Done      int64                `json:"done"`
	Status    store.TransferStatus `json:"status"`
	Error     ErrorCode            `json:"error,omitempty"`
	FileID    string               `json:"fileId,omitempty"`
	CreatedAt string               `json:"createdAt"`
	UpdatedAt string               `json:"updatedAt"`
}

func TransferFrom(t store.Transfer) Transfer {
	return Transfer{
		ID:        t.ID,
		DeviceID:  t.DeviceID,
		Direction: t.Direction,
		Name:      t.Name,
		Size:      t.Size,
		Done:      t.Done,
		Status:    t.Status,
		Error:     ErrorCode(t.Error),
		FileID:    t.FileID,
		CreatedAt: Timestamp(t.CreatedAt),
		UpdatedAt: Timestamp(t.UpdatedAt),
	}
}

type OfferedFile struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt"`
	CreatedAt  string `json:"createdAt"`
}

func FileFrom(f store.File) OfferedFile {
	return OfferedFile{
		ID:         f.ID,
		Name:       f.Name,
		Size:       f.Size,
		ModifiedAt: Timestamp(f.ModTime),
		CreatedAt:  Timestamp(f.CreatedAt),
	}
}

const (
	FileAdded   = "added"
	FileRemoved = "removed"
)

type FileChange struct {
	Action string      `json:"action"`
	File   OfferedFile `json:"file"`
}

type Settings struct {
	Name         string `json:"name"`
	ReceivedDir  string `json:"receivedDir"`
	StartAtLogin bool   `json:"startAtLogin"`
	CheckUpdates bool   `json:"checkUpdates"`
}

type SettingsPatch struct {
	Name         *string `json:"name,omitempty"`
	ReceivedDir  *string `json:"receivedDir,omitempty"`
	StartAtLogin *bool   `json:"startAtLogin,omitempty"`
	CheckUpdates *bool   `json:"checkUpdates,omitempty"`
}

type UpdateStatus struct {
	Current   string `json:"current"`
	Available string `json:"available,omitempty"`
	State     string `json:"state"`
	CheckedAt string `json:"checkedAt,omitempty"`
	Error     string `json:"error,omitempty"`
}

const (
	FirewallAllowed = "allowed"
	FirewallBlocked = "blocked"
	FirewallUnknown = "unknown"
)

type Network struct {
	Firewall string `json:"firewall"`
	Profile  string `json:"profile"`
}

type OfferRequest struct {
	Paths []string `json:"paths"`
}

type OpenReceivedRequest struct {
	TransferID string `json:"transferId"`
}

func Timestamp(t time.Time) string {
	return t.UTC().Format(timestampLayout)
}

func OptionalTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return Timestamp(t)
}
