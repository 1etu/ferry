package update

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

const childEnv = "FERRY_UPDATE_TEST_CHILD"

func TestChildProcess(t *testing.T) {
	switch os.Getenv(childEnv) {
	case "sleep":
		time.Sleep(30 * time.Second)
	case "exit":
	default:
		t.Skip("runs only as the launched child")
	}
}

type childLauncher struct {
	t    *testing.T
	mode string
	exe  string
	args []string
	cmd  *exec.Cmd
}

func (c *childLauncher) launch(exe string, args ...string) (*os.Process, error) {
	c.exe, c.args = exe, args
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(self, "-test.run=^TestChildProcess$")
	cmd.Env = append(os.Environ(), childEnv+"="+c.mode)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c.cmd = cmd
	c.t.Cleanup(func() {
		if err := kill(cmd.Process); err != nil {
			c.t.Log(err)
		}
	})
	return cmd.Process, nil
}

func (c *childLauncher) waitExit() {
	c.t.Helper()
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		c.t.Fatal("child still running after rollback")
	}
}

func stagedFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, "1.0.0", newRelease(t, "1.0.1", []byte("new")))
	writeFile(t, f.u.newExePath(), "new")
	f.u.markStaged("1.0.1")
	f.u.set(func(s *Status) { s.State, s.Available = StateReady, "1.0.1" })
	return f
}

func TestStageRejectsMismatchedAsset(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		alter func(*release)
	}{
		{"size differs", func(r *release) { r.size = 99 }},
		{"hash differs", func(r *release) { r.sha256 = strings.Repeat("0", 64) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newRelease(t, "1.0.1", []byte("new"))
			tc.alter(r)
			f := newFixture(t, "1.0.0", r)

			st := f.u.Check(t.Context())

			if st.State != StateFailed || st.Available != "1.0.1" || !strings.Contains(st.Error, ErrAssetMismatch.Error()) {
				t.Fatalf("got %+v", st)
			}
			if exists(t, f.u.newExePath()) {
				t.Fatal("mismatched download kept")
			}
			if _, err := f.u.Swap(); !errors.Is(err, ErrNotReady) {
				t.Fatalf("Swap after failed stage: %v", err)
			}
		})
	}
}

func TestStageSkipsDownloadWhenFileAlreadyMatches(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "1.0.1", []byte("new"))
	f := newFixture(t, "1.0.0", r)
	writeFile(t, f.u.newExePath(), "new")

	st := f.u.Check(t.Context())

	if st.State != StateReady || f.r.assetRequests() != 0 {
		t.Fatalf("got %+v after %d downloads", st, f.r.assetRequests())
	}
}

func TestStageRetriesAfterFailedDownload(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "1.0.1", []byte("new"))
	r.assetCode = http.StatusNotFound
	f := newFixture(t, "1.0.0", r)
	if st := f.u.Check(t.Context()); st.State != StateFailed {
		t.Fatalf("got %+v", st)
	}
	r.update(func(r *release) { r.assetCode = 0 })

	if err := f.u.Stage(t.Context()); err != nil {
		t.Fatal(err)
	}
	if st := f.u.Status(); st.State != StateReady || st.Available != "1.0.1" {
		t.Fatalf("got %+v", st)
	}
	if got := readFile(t, f.u.newExePath()); got != "new" {
		t.Fatalf("staged %q", got)
	}
}

func TestStageWithoutPendingManifest(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "1.0.0", newRelease(t, "1.0.1", []byte("new")))
	if err := f.u.Stage(t.Context()); !errors.Is(err, ErrNothingPending) {
		t.Fatalf("got %v", err)
	}
}

func TestSwapRenamesAndRestores(t *testing.T) {
	t.Parallel()
	f := stagedFixture(t)

	restore, err := f.u.Swap()
	if err != nil {
		t.Fatal(err)
	}
	if readFile(t, f.exe) != "new" || readFile(t, f.u.oldExePath()) != "old" || exists(t, f.u.newExePath()) {
		t.Fatal("swap left the wrong files")
	}

	if err := restore(); err != nil {
		t.Fatal(err)
	}
	if readFile(t, f.exe) != "old" || readFile(t, f.u.newExePath()) != "new" || exists(t, f.u.oldExePath()) {
		t.Fatal("restore left the wrong files")
	}
}

func TestSwapRefusesWithoutStagedUpdate(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "1.0.0", newRelease(t, "1.0.1", []byte("new")))
	writeFile(t, f.u.newExePath(), "stale")
	if _, err := f.u.Swap(); !errors.Is(err, ErrNotReady) {
		t.Fatalf("got %v", err)
	}
	if readFile(t, f.exe) != "old" {
		t.Fatal("exe replaced by an unverified file")
	}
}

func TestSwapRollsBackWhenNewExeIsMissing(t *testing.T) {
	t.Parallel()
	f := stagedFixture(t)
	if err := os.Remove(f.u.newExePath()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.u.Swap(); err == nil {
		t.Fatal("swap succeeded without the new exe")
	}
	if readFile(t, f.exe) != "old" || exists(t, f.u.oldExePath()) {
		t.Fatal("first rename not undone")
	}
}

func TestApplyRollsBackWhenNewProcessNeverAnswers(t *testing.T) {
	t.Parallel()
	f := stagedFixture(t)
	f.u.healthTimeout = 500 * time.Millisecond
	child := &childLauncher{t: t, mode: "sleep"}
	f.u.launch = child.launch
	health := healthServer(t, "1.0.0")

	err := f.u.Apply(t.Context(), health.URL+"/api/health")

	if err == nil || !strings.Contains(err.Error(), "not healthy") {
		t.Fatalf("got %v", err)
	}
	if child.exe != f.exe || !slices.Equal(child.args, []string{updatedFlag}) {
		t.Fatalf("launched %s %v", child.exe, child.args)
	}
	child.waitExit()
	if readFile(t, f.exe) != "old" || exists(t, f.u.oldExePath()) || exists(t, f.u.newExePath()) {
		t.Fatal("rollback left the wrong files")
	}
	if c := f.cache(t); c.Failed != "1.0.1" {
		t.Fatalf("cache %+v", c)
	}
	if st := f.u.Status(); st.State != StateFailed || st.Available != "1.0.1" || st.Error == "" {
		t.Fatalf("got %+v", st)
	}
	if st := f.u.Check(t.Context()); st.State != StateIdle || f.r.assetRequests() != 0 {
		t.Fatalf("failed version retried: %+v", st)
	}
}

func TestApplyFailsFastWhenNewProcessExits(t *testing.T) {
	t.Parallel()
	f := stagedFixture(t)
	child := &childLauncher{t: t, mode: "exit"}
	f.u.launch = child.launch
	health := healthServer(t, "1.0.0")
	started := time.Now()

	err := f.u.Apply(t.Context(), health.URL+"/api/health")

	if err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("got %v", err)
	}
	if time.Since(started) > 10*time.Second {
		t.Fatal("waited for the full health timeout")
	}
	if readFile(t, f.exe) != "old" {
		t.Fatal("rollback did not restore the exe")
	}
}

func TestApplyKeepsNewVersionThatAnswers(t *testing.T) {
	t.Parallel()
	f := stagedFixture(t)
	child := &childLauncher{t: t, mode: "sleep"}
	f.u.launch = child.launch
	health := healthServer(t, "1.0.1")

	if err := f.u.Apply(t.Context(), health.URL+"/api/health"); err != nil {
		t.Fatal(err)
	}
	if readFile(t, f.exe) != "new" || readFile(t, f.u.oldExePath()) != "old" {
		t.Fatal("swap left the wrong files")
	}
	if st := f.u.Status(); st.State != StateReady {
		t.Fatalf("got %+v", st)
	}
	if err := f.u.Apply(t.Context(), health.URL+"/api/health"); !errors.Is(err, ErrNotReady) || readFile(t, f.exe) != "new" {
		t.Fatalf("second apply: %v", err)
	}
	if err := f.u.RemoveOld(t.Context()); err != nil || exists(t, f.u.oldExePath()) {
		t.Fatalf("RemoveOld: %v", err)
	}
	if err := f.u.RemoveOld(t.Context()); err != nil {
		t.Fatalf("RemoveOld without file: %v", err)
	}
}

func TestApplyRequiresReadyState(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "1.0.0", newRelease(t, "1.0.1", []byte("new")))
	if err := f.u.Apply(t.Context(), "http://127.0.0.1:1/api/health"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("got %v", err)
	}
}
