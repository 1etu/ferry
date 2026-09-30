package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type playwrightSuite struct {
	Title  string
	Specs  []playwrightSpec
	Suites []playwrightSuite
}

type playwrightSpec struct {
	Title string
	File  string
	Tests []struct {
		ProjectName string
		Status      string
		Results     []struct {
			Error struct {
				Message string
			}
		}
	}
}

type playwrightReport struct {
	Stats struct {
		Expected   int
		Unexpected int
		Flaky      int
		Skipped    int
	}
	Suites []playwrightSuite
}

func e2eSuite(grep string) suite {
	return suite{
		name:  "e2e",
		label: "playwright",
		run: func(ctx context.Context, report progress) result {
			return runPlaywright(ctx, grep, report)
		},
	}
}

func runPlaywright(ctx context.Context, grep string, report progress) result {
	dir, err := os.MkdirTemp("", "ferry-e2e-")
	if err != nil {
		return result{err: err}
	}
	defer os.RemoveAll(dir)
	binary := filepath.Join(dir, "Ferry"+exeSuffix())
	report("building web")
	if out, err := exec.CommandContext(ctx, "pnpm", "-C", "web", "build").CombinedOutput(); err != nil {
		return result{err: fmt.Errorf("web build: %w\n%s", err, tail(string(out)))}
	}
	report("building binary")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/ferry")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		return result{err: fmt.Errorf("go build: %w\n%s", err, tail(string(out)))}
	}
	report("running flows")
	reportPath := filepath.Join(dir, "playwright.json")
	args := []string{"-C", "web", "exec", "playwright", "test", "--reporter=json"}
	if grep != "" {
		args = append(args, "--grep", grep)
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "pnpm", args...)
	cmd.Env = append(os.Environ(), "FERRY_BIN="+binary, "PLAYWRIGHT_JSON_OUTPUT_FILE="+reportPath, "PLAYWRIGHT_JSON_OUTPUT_NAME="+reportPath)
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	data, err := os.ReadFile(reportPath)
	if err != nil {
		return result{err: fmt.Errorf("playwright produced no report: %w\n%s", runErr, tail(stderr.String()))}
	}
	var p playwrightReport
	if err := json.Unmarshal(data, &p); err != nil {
		return result{err: fmt.Errorf("read playwright report: %w", err)}
	}
	return playwrightResult(p)
}

func playwrightResult(p playwrightReport) result {
	r := result{passed: p.Stats.Expected + p.Stats.Flaky, failed: p.Stats.Unexpected, skipped: p.Stats.Skipped}
	var walk func(suites []playwrightSuite)
	walk = func(suites []playwrightSuite) {
		for _, s := range suites {
			for _, spec := range s.Specs {
				for _, t := range spec.Tests {
					if t.Status != "unexpected" {
						continue
					}
					var message string
					if len(t.Results) > 0 {
						message = t.Results[len(t.Results)-1].Error.Message
					}
					r.failures = append(r.failures, failure{
						title:  fmt.Sprintf("[%s] %s › %s", t.ProjectName, spec.File, spec.Title),
						detail: firstLines(stripANSI(message), 8),
					})
				}
			}
			walk(s.Suites)
		}
	}
	walk(p.Suites)
	return r
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
