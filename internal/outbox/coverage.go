package outbox

import "slices"

type span struct {
	start, end int64
}

type coverage []span

func coveredPrefix(prefix int64) coverage {
	return coverage{}.add(0, prefix)
}

func (c coverage) add(start, end int64) coverage {
	if end <= start {
		return c
	}
	first := 0
	for first < len(c) && c[first].end < start {
		first++
	}
	last := first
	for last < len(c) && c[last].start <= end {
		start = min(start, c[last].start)
		end = max(end, c[last].end)
		last++
	}
	return slices.Concat(c[:first], coverage{{start: start, end: end}}, c[last:])
}

func (c coverage) prefix() int64 {
	if len(c) == 0 || c[0].start != 0 {
		return 0
	}
	return c[0].end
}
