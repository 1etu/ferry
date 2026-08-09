//go:build windows

package tray

import (
	"context"
	_ "embed"
	"sync"

	"fyne.io/systray"
)

//go:embed icon.ico
var icon []byte

type Actions struct {
	Open         func()
	SendFiles    func()
	OpenReceived func()
	Update       func()
	Quit         func()
}

var updateItem = struct {
	mu      sync.Mutex
	item    *systray.MenuItem
	isReady bool
}{}

func Run(ctx context.Context, a Actions, tooltip string) {
	systray.Run(func() { showMenu(ctx, a, tooltip) }, nil)
}

func SetUpdateReady(isReady bool) {
	updateItem.mu.Lock()
	defer updateItem.mu.Unlock()
	updateItem.isReady = isReady
	showUpdateLocked()
}

func showUpdateLocked() {
	switch {
	case updateItem.item == nil:
	case updateItem.isReady:
		updateItem.item.Show()
	default:
		updateItem.item.Hide()
	}
}

func showMenu(ctx context.Context, a Actions, tooltip string) {
	systray.SetIcon(icon)
	systray.SetTooltip(tooltip)
	systray.SetOnTapped(func() { go a.Open() })
	open := systray.AddMenuItem("Open Ferry", "")
	sendFiles := systray.AddMenuItem("Send Files…", "")
	systray.AddSeparator()
	openReceived := systray.AddMenuItem("Received Files", "")
	systray.AddSeparator()
	update := systray.AddMenuItem("Restart to Update", "")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit Ferry", "")
	updateItem.mu.Lock()
	updateItem.item = update
	showUpdateLocked()
	updateItem.mu.Unlock()
	go func() {
		<-ctx.Done()
		systray.Quit()
	}()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-open.ClickedCh:
				a.Open()
			case <-sendFiles.ClickedCh:
				a.SendFiles()
			case <-openReceived.ClickedCh:
				a.OpenReceived()
			case <-update.ClickedCh:
				a.Update()
			case <-quit.ClickedCh:
				a.Quit()
			}
		}
	}()
}
