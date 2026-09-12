//go:build !windows

package install

import "github.com/1etu/ferry/internal/platform"

func defaultLayout() (layout, error) {
	return layout{}, platform.ErrUnsupported
}
