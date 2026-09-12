package install

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"

	"github.com/1etu/ferry/internal/platform"
)

func defaultLayout() (layout, error) {
	installDir, err := platform.InstallDir()
	if err != nil {
		return layout{}, err
	}
	programsDir, err := windows.KnownFolderPath(windows.FOLDERID_Programs, 0)
	if err != nil {
		return layout{}, fmt.Errorf("resolve start menu programs folder: %w", err)
	}
	sendToDir, err := windows.KnownFolderPath(windows.FOLDERID_SendTo, 0)
	if err != nil {
		return layout{}, fmt.Errorf("resolve send to folder: %w", err)
	}
	return layout{
		installDir:         installDir,
		startMenuLink:      filepath.Join(programsDir, startMenuLinkName),
		sendToLink:         filepath.Join(sendToDir, platform.SendToLinkName),
		registry:           platform.UserRegistryRoot,
		removeDirAfterExit: platform.RemoveDirAfterExit,
	}, nil
}
