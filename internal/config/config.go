package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Name                      string `json:"name"`
	Port                      int    `json:"port"`
	ReceivedDir               string `json:"receivedDir"`
	MaxUploadBytes            int64  `json:"maxUploadBytes"`
	ReserveBytes              int64  `json:"reserveBytes"`
	MaxActiveUploadsPerDevice int    `json:"maxActiveUploadsPerDevice"`
	CheckUpdates              bool   `json:"checkUpdates"`
}

type Env struct {
	DataDir  string
	Headless bool
	Dev      bool
	Port     int
}

const (
	defaultPort                      = 8080
	defaultMaxUploadBytes            = 64 << 30
	defaultReserveBytes              = 2 << 30
	defaultMaxActiveUploadsPerDevice = 8
	maxPort                          = 65535
	incomingDirName                  = ".incoming"
)

func ReadEnv() Env {
	return readEnv(os.Getenv)
}

func readEnv(getenv func(string) string) Env {
	return Env{
		DataDir:  getenv("FERRY_DATA_DIR"),
		Headless: getenv("FERRY_HEADLESS") == "1",
		Dev:      getenv("FERRY_DEV") == "1",
		Port:     portFromEnv(getenv("FERRY_PORT")),
	}
}

func portFromEnv(value string) int {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return port
}

func Defaults(hostname, receivedDir string) Config {
	return Config{
		Name:                      hostname,
		Port:                      defaultPort,
		ReceivedDir:               receivedDir,
		MaxUploadBytes:            defaultMaxUploadBytes,
		ReserveBytes:              defaultReserveBytes,
		MaxActiveUploadsPerDevice: defaultMaxActiveUploadsPerDevice,
		CheckUpdates:              true,
	}
}

func Load(path string, defaults Config) (Config, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := defaults.Save(path); err != nil {
			return Config{}, err
		}
		return defaults, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("open config %s: %w", path, err)
	}
	defer f.Close()
	c, err := decode(f, defaults)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	return c, nil
}

func decode(r io.Reader, defaults Config) (Config, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	c := defaults
	if err := dec.Decode(&c); err != nil {
		return Config{}, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("unexpected data after config object")
	}
	return c, nil
}

func (c Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}
	if err := replaceFile(path, append(data, '\n')); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

func replaceFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	if err := writeAndClose(tmp, data); err != nil {
		return errors.Join(err, os.Remove(tmp.Name()))
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return errors.Join(err, os.Remove(tmp.Name()))
	}
	return nil
}

func writeAndClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Sync(); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}

func (c Config) Validate() error {
	var errs []error
	if strings.TrimSpace(c.Name) == "" {
		errs = append(errs, errors.New("name is empty"))
	}
	if c.Port < 1 || c.Port > maxPort {
		errs = append(errs, fmt.Errorf("port %d is outside 1-%d", c.Port, maxPort))
	}
	if !filepath.IsAbs(c.ReceivedDir) {
		errs = append(errs, fmt.Errorf("receivedDir %q is not an absolute path", c.ReceivedDir))
	}
	if c.MaxUploadBytes <= 0 {
		errs = append(errs, fmt.Errorf("maxUploadBytes %d is not positive", c.MaxUploadBytes))
	}
	if c.ReserveBytes < 0 {
		errs = append(errs, fmt.Errorf("reserveBytes %d is negative", c.ReserveBytes))
	}
	if c.MaxActiveUploadsPerDevice <= 0 {
		errs = append(errs, fmt.Errorf("maxActiveUploadsPerDevice %d is not positive", c.MaxActiveUploadsPerDevice))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return nil
}

func (c Config) IncomingDir() string {
	return filepath.Join(c.ReceivedDir, incomingDirName)
}
