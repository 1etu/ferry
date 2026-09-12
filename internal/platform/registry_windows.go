package platform

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const webView2ClientKey = `Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`

func (r RegistryRoot) runKeyPath() string {
	return string(r) + `\Run`
}

func (r RegistryRoot) uninstallKeyPath() string {
	return string(r) + `\Uninstall\` + appDirName
}

func (r RegistryRoot) RunAtLogin() (bool, error) {
	value, err := readString(registry.CURRENT_USER, r.runKeyPath(), appDirName)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read run at login: %w", err)
	}
	return value != "", nil
}

func (r RegistryRoot) SetRunAtLogin(exePath string, enabled bool) error {
	if !enabled {
		return r.removeRunValue()
	}
	err := writeUserKey(r.runKeyPath(), func(key registry.Key) error {
		return key.SetStringValue(appDirName, `"`+exePath+`" --hidden`)
	})
	if err != nil {
		return fmt.Errorf("enable run at login: %w", err)
	}
	return nil
}

func (r RegistryRoot) removeRunValue() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, r.runKeyPath(), registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open run key: %w", err)
	}
	err = key.DeleteValue(appDirName)
	if errors.Is(err, registry.ErrNotExist) {
		err = nil
	}
	if err = errors.Join(err, key.Close()); err != nil {
		return fmt.Errorf("disable run at login: %w", err)
	}
	return nil
}

func (r RegistryRoot) WriteUninstallEntry(e UninstallEntry) error {
	err := writeUserKey(r.uninstallKeyPath(), func(key registry.Key) error {
		for name, value := range map[string]string{
			"DisplayName":     e.DisplayName,
			"DisplayVersion":  e.DisplayVersion,
			"DisplayIcon":     e.DisplayIcon,
			"Publisher":       e.Publisher,
			"InstallLocation": e.InstallLocation,
			"UninstallString": e.UninstallString,
		} {
			if err := key.SetStringValue(name, value); err != nil {
				return fmt.Errorf("set %s: %w", name, err)
			}
		}
		for name, value := range map[string]uint32{
			"NoModify":      1,
			"NoRepair":      1,
			"EstimatedSize": e.EstimatedSizeKB,
		} {
			if err := key.SetDWordValue(name, value); err != nil {
				return fmt.Errorf("set %s: %w", name, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("write uninstall entry: %w", err)
	}
	return nil
}

func (r RegistryRoot) RemoveUninstallEntry() error {
	err := registry.DeleteKey(registry.CURRENT_USER, r.uninstallKeyPath())
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("remove uninstall entry: %w", err)
	}
	return nil
}

func HasWebView2() bool {
	locations := []struct {
		root registry.Key
		path string
	}{
		{root: registry.LOCAL_MACHINE, path: `SOFTWARE\WOW6432Node\` + webView2ClientKey},
		{root: registry.CURRENT_USER, path: `Software\` + webView2ClientKey},
	}
	for _, location := range locations {
		version, err := readString(location.root, location.path, "pv")
		if err == nil && isWebView2Version(version) {
			return true
		}
	}
	return false
}

func readString(root registry.Key, path, name string) (string, error) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()
	value, _, err := key.GetStringValue(name)
	if err != nil {
		return "", err
	}
	return value, nil
}

func writeUserKey(path string, write func(registry.Key) error) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create key %s: %w", path, err)
	}
	return errors.Join(write(key), key.Close())
}
