package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"slices"
	"strings"
	"time"
)

const usage = `
  ferry test [target…] [options]

  Targets
    go         Go unit tests and the suites in tests/
    web        Web unit tests (Vitest)
    e2e        End-to-end flows (Playwright, builds the app first)
    all        Everything above
    <area>     Go tests for one area, such as server or inbox

  With no target, runs go and web.

  Options
    --race          Enable the Go race detector (needs a C compiler)
    --run <regex>   Only run tests whose names match
    -h, --help      Show this help
`

type plan struct {
	targets []string
	areas   []string
	race    bool
	run     string
}

var errHelp = errors.New("help")

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	p, err := parseArgs(args)
	if errors.Is(err, errHelp) {
		fmt.Print(usage, "\n")
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n  ferry test: %v\n%s\n", err, usage)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return execute(ctx, p)
}

func parseArgs(args []string) (plan, error) {
	var p plan
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "-h", "--help", "help":
			return p, errHelp
		case "--race":
			p.race = true
		case "--run":
			if i+1 >= len(args) {
				return p, errors.New("--run needs a pattern")
			}
			i++
			p.run = args[i]
		case "go", "web", "e2e":
			p.targets = append(p.targets, arg)
		case "all":
			p.targets = append(p.targets, "go", "web", "e2e")
		default:
			if strings.HasPrefix(arg, "-") {
				return p, fmt.Errorf("unknown option %s", arg)
			}
			if !isArea(arg) {
				return p, fmt.Errorf("unknown target %q", arg)
			}
			p.areas = append(p.areas, arg)
		}
	}
	if len(p.targets) == 0 && len(p.areas) == 0 {
		p.targets = []string{"go", "web"}
	}
	slices.Sort(p.targets)
	p.targets = slices.Compact(p.targets)
	return p, nil
}

func isArea(name string) bool {
	return len(areaRoots(name)) > 0
}

func areaRoots(name string) []string {
	var roots []string
	for _, root := range []string{"internal", "tests"} {
		if info, err := fs.Stat(os.DirFS(root), name); err == nil && info.IsDir() {
			roots = append(roots, root)
		}
	}
	return roots
}

func areaPackages(areas []string) []string {
	var packages []string
	for _, area := range areas {
		for _, root := range areaRoots(area) {
			packages = append(packages, "./"+root+"/"+area+"/...")
		}
	}
	return packages
}

func (p plan) suites() []suite {
	var suites []suite
	packages := areaPackages(p.areas)
	if slices.Contains(p.targets, "go") {
		packages = []string{"./..."}
	}
	if len(packages) > 0 {
		suites = append(suites, goSuite(goOptions{packages: packages, race: p.race, run: p.run}))
	}
	if slices.Contains(p.targets, "web") {
		suites = append(suites, webSuite(p.run))
	}
	if slices.Contains(p.targets, "e2e") {
		suites = append(suites, e2eSuite(p.run))
	}
	return suites
}

func execute(ctx context.Context, p plan) int {
	s := newScreen()
	suites := p.suites()
	names := make([]string, 0, len(suites))
	for _, su := range suites {
		names = append(names, su.name)
	}
	s.header(names)
	started := time.Now()
	results := make([]result, 0, len(suites))
	for _, su := range suites {
		report, done := s.track(su.name, su.label)
		r := runSuite(ctx, su, report)
		done(r)
		results = append(results, r)
		if ctx.Err() != nil {
			break
		}
	}
	s.failures(results)
	s.summary(results, time.Since(started))
	for _, r := range results {
		if !r.ok() {
			return 1
		}
	}
	return 0
}
