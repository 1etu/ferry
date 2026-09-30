package main

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

type palette struct {
	enabled bool
}

func (p palette) wrap(code, text string) string {
	if !p.enabled {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (p palette) bold(text string) string  { return p.wrap("1", text) }
func (p palette) dim(text string) string   { return p.wrap("2", text) }
func (p palette) green(text string) string { return p.wrap("32", text) }
func (p palette) red(text string) string   { return p.wrap("31", text) }

type screen struct {
	out         io.Writer
	color       palette
	interactive bool
}

func newScreen() screen {
	interactive := isTerminal(os.Stdout)
	return screen{
		out:         os.Stdout,
		color:       palette{enabled: interactive && os.Getenv("NO_COLOR") == ""},
		interactive: interactive,
	}
}

func (s screen) header(targets []string) {
	fmt.Fprintf(s.out, "\n  %s %s\n\n", s.color.bold("ferry test"), s.color.dim(strings.Join(targets, " · ")))
}

func (s screen) track(name, label string) (report progress, done func(result)) {
	if !s.interactive {
		return func(string) {}, func(r result) { fmt.Fprintln(s.out, s.line(r)) }
	}
	var mu sync.Mutex
	status := label
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for frame := 0; ; frame++ {
			mu.Lock()
			text := status
			mu.Unlock()
			fmt.Fprintf(s.out, "\r\x1b[2K  %s %s  %s", s.color.dim(spinnerFrames[frame%len(spinnerFrames)]), s.color.bold(pad(name)), s.color.dim(text))
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	}()
	report = func(next string) {
		mu.Lock()
		status = label + " · " + next
		mu.Unlock()
	}
	done = func(r result) {
		close(stop)
		<-finished
		fmt.Fprintf(s.out, "\r\x1b[2K%s\n", s.line(r))
	}
	return report, done
}

func (s screen) line(r result) string {
	mark := s.color.green("✓")
	if !r.ok() {
		mark = s.color.red("✗")
	}
	var parts []string
	if r.err != nil {
		parts = append(parts, s.color.red("could not run"))
	}
	if r.failed > 0 {
		parts = append(parts, s.color.red(fmt.Sprintf("%d failed", r.failed)))
	}
	parts = append(parts, fmt.Sprintf("%d passed", r.passed))
	if r.skipped > 0 {
		parts = append(parts, s.color.dim(fmt.Sprintf("%d skipped", r.skipped)))
	}
	return fmt.Sprintf("  %s %s  %s  %s", mark, s.color.bold(pad(r.name)), strings.Join(parts, s.color.dim(" · ")), s.color.dim(duration(r.elapsed)))
}

func (s screen) failures(results []result) {
	for _, r := range results {
		if r.err != nil {
			fmt.Fprintf(s.out, "\n  %s %s\n%s\n", s.color.red("✗"), s.color.bold(r.name+" could not run"), indent(r.err.Error()))
		}
		for _, f := range r.failures {
			fmt.Fprintf(s.out, "\n  %s %s\n", s.color.red("✗"), s.color.bold(f.title))
			if f.detail != "" {
				fmt.Fprintln(s.out, s.color.dim(indent(f.detail)))
			}
		}
	}
}

func (s screen) summary(results []result, elapsed time.Duration) {
	passed, failed := 0, 0
	broken := false
	for _, r := range results {
		passed += r.passed
		failed += r.failed
		broken = broken || r.err != nil
	}
	if failed == 0 && !broken {
		fmt.Fprintf(s.out, "\n  %s %s %s\n\n", s.color.green("✓"), s.color.bold(fmt.Sprintf("%d passed", passed)), s.color.dim("in "+duration(elapsed)))
		return
	}
	fmt.Fprintf(s.out, "\n  %s %s %s %s\n\n", s.color.red("✗"), s.color.bold(fmt.Sprintf("%d failed", failed)), s.color.dim(fmt.Sprintf("· %d passed", passed)), s.color.dim("in "+duration(elapsed)))
}

func pad(name string) string {
	return fmt.Sprintf("%-4s", name)
}

func duration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
}

func indent(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}

func stripANSI(text string) string {
	return ansiPattern.ReplaceAllString(text, "")
}
