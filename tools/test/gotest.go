package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type goEvent struct {
	Action      string
	Package     string
	Test        string
	Output      string
	ImportPath  string
	FailedBuild string
}

type goOptions struct {
	packages []string
	race     bool
	run      string
}

func goSuite(opts goOptions) suite {
	return suite{
		name:  "go",
		label: goLabel(opts.packages),
		run: func(ctx context.Context, report progress) result {
			return runGo(ctx, opts, report)
		},
	}
}

func goLabel(packages []string) string {
	if len(packages) == 1 && packages[0] == "./..." {
		return "all packages"
	}
	names := make([]string, 0, len(packages))
	for _, p := range packages {
		names = append(names, strings.TrimSuffix(strings.TrimPrefix(p, "./"), "/..."))
	}
	return strings.Join(names, ", ")
}

func runGo(ctx context.Context, opts goOptions, report progress) result {
	args := []string{"test", "-json", "-count=1"}
	if opts.race {
		args = append(args, "-race")
	}
	if opts.run != "" {
		args = append(args, "-run", opts.run)
	}
	args = append(args, opts.packages...)
	cmd := exec.CommandContext(ctx, "go", args...)
	if opts.race {
		cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	}
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return result{err: err}
	}
	if err := cmd.Start(); err != nil {
		return result{err: fmt.Errorf("start go test: %w", err)}
	}
	r := parseGoEvents(stdout, report)
	waitErr := cmd.Wait()
	if waitErr != nil && r.failed == 0 && r.err == nil {
		r.err = fmt.Errorf("go test: %w", waitErr)
	}
	return r
}

func parseGoEvents(stream io.Reader, report progress) result {
	var r result
	outputs := map[string][]string{}
	builds := map[string][]string{}
	packagesDone := 0
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		var e goEvent
		if json.Unmarshal(scanner.Bytes(), &e) != nil {
			continue
		}
		key := e.Package + "\x00" + e.Test
		switch e.Action {
		case "build-output":
			builds[e.ImportPath] = append(builds[e.ImportPath], e.Output)
		case "output":
			if e.Test != "" {
				outputs[key] = append(outputs[key], e.Output)
			}
		case "pass", "fail", "skip":
			if e.Test == "" {
				if e.Action == "fail" && e.FailedBuild != "" {
					r.failed++
					r.failures = append(r.failures, failure{title: shortPackage(e.Package) + " (build failed)", detail: strings.Join(builds[e.FailedBuild], "")})
				}
				packagesDone++
				report(fmt.Sprintf("%d packages, %d tests", packagesDone, r.passed+r.failed))
				continue
			}
			countGoTest(&r, e, outputs[key])
			delete(outputs, key)
		}
	}
	sort.Slice(r.failures, func(i, j int) bool { return r.failures[i].title < r.failures[j].title })
	return r
}

func countGoTest(r *result, e goEvent, output []string) {
	switch e.Action {
	case "pass":
		if !strings.Contains(e.Test, "/") {
			r.passed++
		}
	case "skip":
		if !strings.Contains(e.Test, "/") {
			r.skipped++
		}
	case "fail":
		if strings.Contains(e.Test, "/") && hasParentFailure(r, e) {
			return
		}
		r.failed++
		r.failures = append(r.failures, failure{
			title:  shortPackage(e.Package) + " › " + e.Test,
			detail: goFailureDetail(output),
		})
	}
}

func hasParentFailure(r *result, e goEvent) bool {
	parent := shortPackage(e.Package) + " › " + strings.SplitN(e.Test, "/", 2)[0]
	for _, f := range r.failures {
		if f.title == parent {
			return true
		}
	}
	return false
}

func goFailureDetail(output []string) string {
	var lines []string
	for _, line := range output {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "=== ") || strings.HasPrefix(trimmed, "--- ") {
			continue
		}
		lines = append(lines, trimmed)
	}
	return strings.Join(lines, "\n")
}

func shortPackage(path string) string {
	if i := strings.Index(path, "/internal/"); i >= 0 {
		return path[i+1:]
	}
	if i := strings.Index(path, "/tests/"); i >= 0 {
		return path[i+1:]
	}
	return filepath.Base(path)
}
