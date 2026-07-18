package auth

import (
	"context"

	"github.com/1etu/ferry/internal/store"
)

type Role int

const (
	RoleNone Role = iota
	RoleDevice
	RoleOwner
)

type Principal struct {
	Role   Role
	Device store.Device
}

type principalKey struct{}

func FromContext(ctx context.Context) Principal {
	p, ok := ctx.Value(principalKey{}).(Principal)
	if !ok {
		return Principal{}
	}
	return p
}

func withPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func (p Principal) isApprovedDevice() bool {
	return p.Role == RoleDevice && p.Device.Status == store.DeviceApproved
}

func (p Principal) isPendingDevice() bool {
	return p.Role == RoleDevice && p.Device.Status == store.DevicePending
}
