package main

import (
	"context"
	"time"
)

type failure struct {
	title  string
	detail string
}

type result struct {
	name     string
	label    string
	passed   int
	failed   int
	skipped  int
	elapsed  time.Duration
	failures []failure
	err      error
}

func (r result) ok() bool {
	return r.err == nil && r.failed == 0
}

type progress func(status string)

type suite struct {
	name  string
	label string
	run   func(ctx context.Context, report progress) result
}

func runSuite(ctx context.Context, s suite, report progress) result {
	started := time.Now()
	r := s.run(ctx, report)
	r.name = s.name
	if r.label == "" {
		r.label = s.label
	}
	r.elapsed = time.Since(started)
	return r
}
