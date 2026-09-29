package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/1etu/ferry/internal/seal"
)

const (
	megabyte       = 1000 * 1000
	mebibyte       = 1 << 20
	startupTimeout = 15 * time.Second
	quitTimeout    = 60 * time.Second
	healthPoll     = 100 * time.Millisecond
)

type options struct {
	binary   string
	scenario string
	runs     int
	settle   time.Duration
	size     int64
	files    int
	fileSize int64
	parallel int
	chunk    int64
	verify   bool
}

type result struct {
	elapsed   time.Duration
	bytes     int64
	files     int
	latencies []time.Duration
}

type scenario func(ctx context.Context, s *server, d *device, o options) (result, error)

var scenarios = map[string]scenario{
	"upload":   runUpload,
	"burst":    runBurst,
	"download": runDownload,
	"idle":     runIdle,
}

type server struct {
	url      string
	root     string
	received string
	scratch  string
	client   *http.Client
}

type process struct {
	cmd    *exec.Cmd
	exited chan error
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	o, err := parseOptions(args)
	if err != nil {
		return err
	}
	measure, ok := scenarios[o.scenario]
	if !ok {
		return fmt.Errorf("unknown scenario %q, want upload, burst, download or idle", o.scenario)
	}
	ctx := context.Background()
	for i := 1; i <= o.runs; i++ {
		if err := runOnce(ctx, out, o, i, measure); err != nil {
			return fmt.Errorf("%s run %d: %w", o.scenario, i, err)
		}
	}
	return nil
}

func parseOptions(args []string) (options, error) {
	var o options
	flags := flag.NewFlagSet("bench", flag.ContinueOnError)
	flags.StringVar(&o.binary, "bin", filepath.Join("bin", "Ferry.exe"), "Ferry binary to launch headless")
	flags.StringVar(&o.scenario, "scenario", "upload", "upload, burst, download or idle")
	flags.IntVar(&o.runs, "runs", 1, "runs, each against a fresh server")
	flags.DurationVar(&o.settle, "settle", 20*time.Second, "pause before the timed part, for disk write-back")
	flags.Int64Var(&o.size, "size", 2<<30, "bytes of the single upload or download")
	flags.IntVar(&o.files, "files", 500, "files in a burst")
	flags.Int64Var(&o.fileSize, "file-size", 3*megabyte, "bytes per burst file")
	flags.IntVar(&o.parallel, "parallel", 4, "uploads in flight during a burst")
	flags.Int64Var(&o.chunk, "chunk", 16*mebibyte, "plaintext bytes per request")
	flags.BoolVar(&o.verify, "verify", true, "compare the received file's SHA-256 with the source")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if o.chunk <= 0 || o.chunk%seal.FrameSize != 0 {
		return options{}, fmt.Errorf("chunk %d is not a positive multiple of %d", o.chunk, seal.FrameSize)
	}
	if o.fileSize > o.chunk {
		return options{}, fmt.Errorf("file size %d exceeds the chunk, a burst file must fit in its creation request", o.fileSize)
	}
	return o, nil
}

func runOnce(ctx context.Context, out io.Writer, o options, index int, measure scenario) error {
	s, err := newServer()
	if err != nil {
		return err
	}
	p, err := s.launch(ctx, o.binary)
	if err != nil {
		return errors.Join(err, s.cleanup())
	}
	res, err := s.measure(ctx, p, o, measure)
	if err := errors.Join(err, s.cleanup()); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "%s run %d: %s%s\n", o.scenario, index, res.summary(), p.cpu(res.bytes))
	return err
}

func (s *server) measure(ctx context.Context, p *process, o options, measure scenario) (result, error) {
	d, err := pair(ctx, s)
	if err != nil {
		return result{}, errors.Join(err, s.stop(ctx, p))
	}
	res, err := measure(ctx, s, d, o)
	return res, errors.Join(err, s.stop(ctx, p))
}

func newServer() (*server, error) {
	root, err := os.MkdirTemp("", "ferry-bench-")
	if err != nil {
		return nil, fmt.Errorf("create bench dir: %w", err)
	}
	s := &server{
		root:     root,
		received: filepath.Join(root, "received"),
		scratch:  filepath.Join(root, "scratch"),
		client:   newClient(),
	}
	for _, dir := range []string{s.received, s.scratch, filepath.Join(root, "data")} {
		if err := os.Mkdir(dir, 0o750); err != nil {
			return nil, errors.Join(fmt.Errorf("create %s: %w", dir, err), s.cleanup())
		}
	}
	return s, nil
}

func newClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		MaxIdleConnsPerHost: 16,
		DisableCompression:  true,
		WriteBufferSize:     256 << 10,
		ReadBufferSize:      256 << 10,
	}}
}

func (s *server) launch(ctx context.Context, binary string) (*process, error) {
	dataDir := filepath.Join(s.root, "data")
	config, err := json.Marshal(map[string]string{"receivedDir": s.received})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dataDir, "config.json"), config, 0o600); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}
	port, err := freePort(ctx)
	if err != nil {
		return nil, err
	}
	logFile, err := os.Create(filepath.Join(s.root, "ferry.out"))
	if err != nil {
		return nil, fmt.Errorf("create output log: %w", err)
	}
	defer logFile.Close()
	s.url = "http://127.0.0.1:" + strconv.Itoa(port)
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = append(os.Environ(), "FERRY_DATA_DIR="+dataDir, "FERRY_PORT="+strconv.Itoa(port), "FERRY_HEADLESS=1", "FERRY_DEV=1")
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", binary, err)
	}
	p := &process{cmd: cmd, exited: make(chan error, 1)}
	go func() { p.exited <- cmd.Wait() }()
	return p, s.awaitHealthy(ctx, p)
}

func freePort(ctx context.Context) (int, error) {
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find a free port: %w", err)
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("listener address %T", listener.Addr())
	}
	return addr.Port, nil
}

func (s *server) awaitHealthy(ctx context.Context, p *process) error {
	deadline := time.Now().Add(startupTimeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-p.exited:
			return fmt.Errorf("ferry exited during startup: %w", err)
		default:
		}
		if err := s.owner(ctx, http.MethodGet, "/api/health", nil, nil); err == nil {
			return nil
		}
		time.Sleep(healthPoll)
	}
	return errors.Join(fmt.Errorf("ferry not healthy at %s after %s", s.url, startupTimeout), p.cmd.Process.Kill())
}

func (s *server) stop(ctx context.Context, p *process) error {
	if err := s.owner(ctx, http.MethodPost, "/api/app/quit", nil, nil); err != nil {
		return errors.Join(err, p.cmd.Process.Kill())
	}
	select {
	case <-p.exited:
		return nil
	case <-time.After(quitTimeout):
		return errors.Join(errors.New("ferry did not quit"), p.cmd.Process.Kill())
	}
}

func (s *server) cleanup() error {
	var err error
	for range 20 {
		if err = os.RemoveAll(s.root); err == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("remove %s: %w", s.root, err)
}

func (p *process) cpu(bytes int64) string {
	user, system := p.cmd.ProcessState.UserTime(), p.cmd.ProcessState.SystemTime()
	line := fmt.Sprintf(", server cpu %.2f+%.2f s", user.Seconds(), system.Seconds())
	if bytes == 0 {
		return line
	}
	return line + fmt.Sprintf(" = %.2f cpu-s/GB", (user+system).Seconds()/(float64(bytes)/1e9))
}

func (r result) summary() string {
	if r.bytes == 0 {
		return "paired, sealed and quit"
	}
	seconds := r.elapsed.Seconds()
	line := fmt.Sprintf("%d MiB in %.2f s = %.0f MB/s", r.bytes/mebibyte, seconds, float64(r.bytes)/megabyte/seconds)
	if r.files > 0 {
		line += fmt.Sprintf(", %d files = %.0f files/s", r.files, float64(r.files)/seconds)
	}
	if len(r.latencies) > 0 {
		sorted := slices.Sorted(slices.Values(r.latencies))
		line += fmt.Sprintf(", %d requests p50 %s p95 %s", len(sorted), percentile(sorted, 50), percentile(sorted, 95))
	}
	return line
}

func percentile(sorted []time.Duration, p int) time.Duration {
	return sorted[(len(sorted)-1)*p/100].Round(100 * time.Microsecond)
}

func runIdle(context.Context, *server, *device, options) (result, error) {
	return result{}, nil
}
