package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type vitestReport struct {
	NumPassedTests  int
	NumFailedTests  int
	NumPendingTests int
	TestResults     []struct {
		Name             string
		Message          string
		AssertionResults []struct {
			FullName        string
			Status          string
			FailureMessages []string
		}
	}
}

func webSuite(filter string) suite {
	return suite{
		name:  "web",
		label: "vitest",
		run: func(ctx context.Context, report progress) result {
			return runVitest(ctx, filter, report)
		},
	}
}

func runVitest(ctx context.Context, filter string, report progress) result {
	dir, err := os.MkdirTemp("", "ferry-test-")
	if err != nil {
		return result{err: err}
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "vitest.json")
	args := []string{"-C", "web", "exec", "vitest", "run", "--reporter=json", "--outputFile=" + out}
	if filter != "" {
		args = append(args, filter)
	}
	report("running")
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "pnpm", args...)
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	data, err := os.ReadFile(out)
	if err != nil {
		return result{err: fmt.Errorf("vitest produced no report: %w\n%s", runErr, tail(stderr.String()))}
	}
	var v vitestReport
	if err := json.Unmarshal(data, &v); err != nil {
		return result{err: fmt.Errorf("read vitest report: %w", err)}
	}
	return vitestResult(v)
}

func vitestResult(v vitestReport) result {
	r := result{passed: v.NumPassedTests, failed: v.NumFailedTests, skipped: v.NumPendingTests}
	for _, file := range v.TestResults {
		name := relativeWeb(file.Name)
		if len(file.AssertionResults) == 0 && file.Message != "" {
			r.failed++
			r.failures = append(r.failures, failure{title: name, detail: file.Message})
		}
		for _, a := range file.AssertionResults {
			if a.Status != "failed" {
				continue
			}
			r.failures = append(r.failures, failure{
				title:  name + " › " + a.FullName,
				detail: firstLines(strings.Join(a.FailureMessages, "\n"), 8),
			})
		}
	}
	return r
}

func relativeWeb(path string) string {
	path = filepath.ToSlash(path)
	if i := strings.Index(path, "/web/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func firstLines(text string, n int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func tail(text string) string {
	const n = 12
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
