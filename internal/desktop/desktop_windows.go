//go:build windows

package desktop

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"github.com/1etu/ferry/internal/platform"
)

const (
	defaultWidth      = 780
	defaultHeight     = 540
	minWidth          = 600
	minHeight         = 420
	appIconID         = 1
	closeTimeout      = 3 * time.Second
	backgroundEnvName = "WEBVIEW2_DEFAULT_BACKGROUND_COLOR"
	backgroundARGB    = "FF1E1E1E"
)

type Window struct {
	url     string
	title   string
	dataDir string
	log     *slog.Logger
	user32  user32

	mu        sync.Mutex
	view      webview2.WebView
	done      chan struct{}
	isClosing bool
}

func New(url, title, dataDir string, log *slog.Logger) *Window {
	return &Window{url: url, title: title, dataDir: dataDir, log: log, user32: loadUser32()}
}

func (w *Window) Available() bool {
	return platform.HasWebView2()
}

func (w *Window) Show() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.isClosing = false
	if w.view != nil {
		view := w.view
		view.Dispatch(func() { w.foreground(view) })
		return
	}
	if w.done != nil {
		return
	}
	w.done = make(chan struct{})
	go w.run(w.done)
}

func (w *Window) Close() {
	w.mu.Lock()
	view, done := w.view, w.done
	if done != nil {
		w.isClosing = true
	}
	w.mu.Unlock()
	if done == nil {
		return
	}
	if view != nil {
		view.Dispatch(view.Terminate)
	}
	select {
	case <-done:
	case <-time.After(closeTimeout):
		w.log.Warn("window still open after close timeout", "duration_ms", closeTimeout.Milliseconds())
	}
}

func (w *Window) run(done chan struct{}) {
	runtime.LockOSThread()
	defer close(done)
	defer w.detach()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil && !errors.Is(err, windows.Errno(windows.S_FALSE)) {
		w.log.Error("window not opened: com initialization failed", "err", err)
		return
	}
	defer windows.CoUninitialize()
	view := w.create()
	if view == nil {
		w.log.Error("window not opened: webview2 creation failed")
		return
	}
	hwnd := windows.HWND(uintptr(view.Window()))
	defer w.destroy(hwnd)
	if !w.attach(view) {
		return
	}
	view.Run()
}

func (w *Window) create() webview2.WebView {
	if err := os.Setenv(backgroundEnvName, backgroundARGB); err != nil {
		w.log.Warn("webview background stays white until the page paints", "err", err)
	}
	dpi := w.user32.systemDPI()
	view := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  filepath.Join(w.dataDir, "webview"),
		WindowOptions: webview2.WindowOptions{
			Title:  w.title,
			Width:  scaleToDPI(defaultWidth, dpi),
			Height: scaleToDPI(defaultHeight, dpi),
			IconId: appIconID,
			Center: true,
		},
	})
	if view == nil {
		return nil
	}
	w.colorTitleBar(windows.HWND(uintptr(view.Window())))
	view.SetSize(int(scaleToDPI(minWidth, dpi)), int(scaleToDPI(minHeight, dpi)), webview2.HintMin)
	view.Navigate(w.url)
	return view
}

func (w *Window) colorTitleBar(hwnd windows.HWND) {
	if err := useDarkTitleBar(hwnd); err != nil {
		w.log.Warn("title bar stays light: dark mode rejected", "err", err)
	}
	if err := paintCaption(hwnd); err != nil {
		w.log.Debug("caption keeps the immersive dark color: caption color rejected", "err", err)
	}
}

func (w *Window) foreground(view webview2.WebView) {
	if err := w.user32.bringToFront(windows.HWND(uintptr(view.Window()))); err != nil {
		w.log.Debug("window shown behind the foreground app", "err", err)
	}
}

func (w *Window) destroy(hwnd windows.HWND) {
	if err := w.user32.destroy(hwnd); err != nil {
		w.log.Warn("window left for thread exit to destroy", "err", err)
	}
}

func (w *Window) attach(view webview2.WebView) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.isClosing {
		return false
	}
	w.view = view
	return true
}

func (w *Window) detach() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.view = nil
	w.done = nil
	w.isClosing = false
}
