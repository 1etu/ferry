package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	"github.com/1etu/ferry/internal/api"
	"github.com/1etu/ferry/internal/config"
	"github.com/1etu/ferry/internal/desktop"
	"github.com/1etu/ferry/internal/events"
	"github.com/1etu/ferry/internal/platform"
	"github.com/1etu/ferry/internal/seal"
	"github.com/1etu/ferry/internal/update"
)

var Version = "dev"

const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2

	configFileName = "config.json"
	loopbackHost   = "127.0.0.1"
	pickTitle      = "Send to iPhone"
	folderTitle    = "Received files"
	windowTitle    = "Ferry"
)

type launch struct {
	showWindow bool
	isUpdated  bool
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if l, isServer := serverLaunch(args); isServer {
		return runServer(ctx, stderr, l)
	}
	switch {
	case len(args) == 1 && args[0] == "uninstall":
		return runUninstall(ctx, stderr)
	case len(args) > 1 && args[0] == "send":
		return runSend(ctx, args[1:])
	case len(args) == 1 && args[0] == "version":
		if _, err := fmt.Fprintln(stdout, Version); err != nil {
			return exitFailure
		}
		return exitOK
	default:
		return exitUsage
	}
}

func dataDir(env config.Env) (string, error) {
	if env.DataDir != "" {
		return env.DataDir, nil
	}
	return platform.DataDir()
}

func defaultConfig(hostname string) (config.Config, error) {
	received, err := platform.DefaultReceivedDir()
	if err != nil {
		return config.Config{}, err
	}
	return config.Defaults(hostname, received), nil
}

func loadConfig(env config.Env, dir, hostname string) (config.Config, error) {
	defaults, err := defaultConfig(hostname)
	if err != nil {
		return config.Config{}, err
	}
	cfg, err := config.Load(filepath.Join(dir, configFileName), defaults)
	if err != nil {
		return config.Config{}, err
	}
	if env.Port != 0 {
		cfg.Port = env.Port
	}
	return cfg, cfg.Validate()
}

func loopbackURL(port int) string {
	return "http://" + loopbackHost + ":" + strconv.Itoa(port)
}

func isRunning(ctx context.Context, client *http.Client, baseURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/health", http.NoBody)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var health api.Health
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&health) != nil {
		return false
	}
	return health.App == api.AppName
}

func reportFailure(stderr io.Writer, err error) {
	slog.New(slog.NewTextHandler(stderr, nil)).Error("ferry failed", "err", err)
}

type window interface {
	Show()
	Close()
	Available() bool
}

type machine struct {
	env        config.Env
	dataDir    string
	hostname   string
	listenHost string
	port       int
	exePath    string
	log        *slog.Logger
	listLAN    func() ([]net.IP, error)
	openURL    func(url string) error
	window     window
	platform   platformCalls

	hub      *events.Hub
	sessions *seal.Sessions
	updater  *update.Updater
	stack    *stack
	network  networkState
	quit     context.CancelFunc

	cfgMu sync.Mutex
	cfg   config.Config

	lifecycle sync.Mutex
}

func newMachine(env config.Env, dir, listenHost string, log *slog.Logger) (*machine, error) {
	hostname, err := platform.Hostname()
	if err != nil {
		return nil, err
	}
	cfg, err := loadConfig(env, dir, hostname)
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate running exe: %w", err)
	}
	m := &machine{
		env:        env,
		dataDir:    dir,
		hostname:   hostname,
		listenHost: listenHost,
		port:       cfg.Port,
		exePath:    exe,
		log:        log,
		listLAN:    platform.LANAddrs,
		openURL:    platform.OpenURL,
		platform:   systemCalls(),
		cfg:        cfg,
	}
	m.window = desktop.New(m.ownerURL(), windowTitle, dir, log)
	return m, nil
}

func (m *machine) config() config.Config {
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	return m.cfg
}

func (m *machine) setConfig(cfg config.Config) {
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	m.cfg = cfg
}

func (m *machine) name() string {
	return m.config().Name
}

func (m *machine) lanAddrs() []net.IP {
	addrs, err := m.listLAN()
	if err != nil {
		m.log.Warn("lan addresses not listed", "err", err)
		return nil
	}
	return addrs
}

func (m *machine) hosts() []string {
	addrs := m.lanAddrs()
	hosts := make([]string, 0, len(addrs)+2)
	hosts = append(hosts, m.hostname, m.hostname+".local")
	for _, ip := range addrs {
		hosts = append(hosts, ip.String())
	}
	return hosts
}

func (m *machine) origins() (local, ip string) {
	port := strconv.Itoa(m.port)
	host := loopbackHost
	if addrs := m.lanAddrs(); len(addrs) > 0 {
		host = addrs[0].String()
	}
	return "http://" + m.hostname + ".local:" + port, "http://" + host + ":" + port
}

func (m *machine) ownerURL() string {
	return loopbackURL(m.port) + "/"
}

func (m *machine) healthURL() string {
	return loopbackURL(m.port) + "/api/health"
}

func serverLaunch(args []string) (launch, bool) {
	switch {
	case len(args) == 0:
		return launch{showWindow: true}, true
	case len(args) == 1 && args[0] == "--hidden":
		return launch{}, true
	case len(args) == 1 && args[0] == "--updated":
		return launch{showWindow: true, isUpdated: true}, true
	default:
		return launch{}, false
	}
}
