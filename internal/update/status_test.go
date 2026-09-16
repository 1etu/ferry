package update

import (
	"encoding/json"
	"testing"
)

func TestNewStartsIdleWhenItCanCheck(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "1.0.0", newRelease(t, "1.0.1", []byte("new")))
	if st := f.u.Status(); st.State != StateIdle || st.Current != "1.0.0" || !st.CheckedAt.IsZero() {
		t.Fatalf("got %+v, want idle before the first check", st)
	}
	if n := f.r.count(manifestPath); n != 0 {
		t.Fatalf("%d requests before Run or Check", n)
	}
}

func TestStatusJSONFollowsContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status Status
		want   string
	}{
		{"minimal", Status{Current: "1.0.0", State: StateDisabled}, `{"current":"1.0.0","state":"disabled"}`},
		{
			"full",
			Status{Current: "1.0.0", Available: "1.0.1", State: StateFailed, CheckedAt: fixedNow(), Error: "boom"},
			`{"current":"1.0.0","available":"1.0.1","state":"failed","checkedAt":"2026-10-02T12:00:00Z","error":"boom"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := json.Marshal(tc.status)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}
