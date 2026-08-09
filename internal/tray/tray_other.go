//go:build !windows

package tray

import "context"

type Actions struct {
	Open         func()
	SendFiles    func()
	OpenReceived func()
	Update       func()
	Quit         func()
}

func Run(ctx context.Context, _ Actions, _ string) {
	<-ctx.Done()
}

func SetUpdateReady(bool) {}
