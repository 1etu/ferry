package outbox

import (
	"slices"
	"testing"
)

func TestCoverage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		spans      []span
		want       coverage
		wantPrefix int64
	}{
		{name: "nothing served covers nothing", want: nil, wantPrefix: 0},
		{name: "an empty span is ignored", spans: []span{{5, 5}, {9, 3}}, want: nil, wantPrefix: 0},
		{name: "a leading span is the prefix", spans: []span{{0, 100}}, want: coverage{{0, 100}}, wantPrefix: 100},
		{name: "a tail span alone leaves no prefix", spans: []span{{900, 1000}}, want: coverage{{900, 1000}}, wantPrefix: 0},
		{
			name:       "disjoint spans stay sorted",
			spans:      []span{{500, 600}, {0, 100}, {800, 900}, {200, 300}},
			want:       coverage{{0, 100}, {200, 300}, {500, 600}, {800, 900}},
			wantPrefix: 100,
		},
		{
			name:       "adjacent spans merge",
			spans:      []span{{0, 100}, {100, 200}},
			want:       coverage{{0, 200}},
			wantPrefix: 200,
		},
		{
			name:       "an overlapping span extends both ways",
			spans:      []span{{100, 200}, {50, 250}},
			want:       coverage{{50, 250}},
			wantPrefix: 0,
		},
		{
			name:       "a span bridging a gap merges its neighbors",
			spans:      []span{{0, 100}, {300, 400}, {600, 700}, {90, 310}},
			want:       coverage{{0, 400}, {600, 700}},
			wantPrefix: 400,
		},
		{
			name:       "a span inside covered bytes changes nothing",
			spans:      []span{{0, 1000}, {200, 300}},
			want:       coverage{{0, 1000}},
			wantPrefix: 1000,
		},
		{
			name:       "the head arriving after the tail completes the prefix",
			spans:      []span{{600, 1000}, {0, 600}},
			want:       coverage{{0, 1000}},
			wantPrefix: 1000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got coverage
			for _, s := range tt.spans {
				got = got.add(s.start, s.end)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			if p := got.prefix(); p != tt.wantPrefix {
				t.Fatalf("got prefix %d, want %d", p, tt.wantPrefix)
			}
		})
	}
}

func TestCoveredPrefixSeedsFromStoredProgress(t *testing.T) {
	t.Parallel()
	if got := coveredPrefix(0); len(got) != 0 {
		t.Fatalf("got %v from no progress, want nothing covered", got)
	}
	resumed := coveredPrefix(600).add(600, 1000)
	if got := resumed.prefix(); got != 1000 {
		t.Fatalf("got prefix %d after resuming at 600, want 1000", got)
	}
}
