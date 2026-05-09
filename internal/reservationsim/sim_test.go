package reservationsim

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// stepClock is a tiny mutable Clock for tests.
type stepClock struct{ now time.Time }

func (c *stepClock) Now() time.Time          { return c.now }
func (c *stepClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newClockAt(t time.Time) *stepClock { return &stepClock{now: t} }

func anchor() time.Time { return time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC) }

func TestAcquire_ValidatesRequest(t *testing.T) {
	t.Parallel()
	clk := newClockAt(anchor())
	sim := NewSimulator(clk)

	cases := []AcquireRequest{
		{PathPattern: "", AgentName: "A", Exclusive: true, TTL: time.Hour},
		{PathPattern: "p", AgentName: "", Exclusive: true, TTL: time.Hour},
		{PathPattern: "p", AgentName: "A", Exclusive: true, TTL: 0},
	}
	for i, req := range cases {
		got := sim.Acquire(req)
		if got.Outcome != OutcomeInvalid {
			t.Errorf("case %d: Outcome = %s, want invalid", i, got.Outcome)
		}
	}
}

func TestAcquire_ExclusiveOnEmptySimulatorIsAcquired(t *testing.T) {
	t.Parallel()
	sim := NewSimulator(newClockAt(anchor()))
	r := sim.Acquire(AcquireRequest{
		PathPattern: "internal/auth/**",
		AgentName:   "Alice",
		Exclusive:   true,
		TTL:         time.Hour,
	})
	if r.Outcome != OutcomeAcquired {
		t.Fatalf("Outcome = %s, want acquired", r.Outcome)
	}
	if r.Lease == nil || r.Lease.AgentName != "Alice" {
		t.Errorf("Lease = %+v, want Alice's lease", r.Lease)
	}
	if !sim.Active()[0].Exclusive {
		t.Errorf("active lease should be exclusive")
	}
}

func TestAcquire_OverlappingExclusiveConflicts(t *testing.T) {
	t.Parallel()
	sim := NewSimulator(newClockAt(anchor()))
	sim.Acquire(AcquireRequest{
		PathPattern: "internal/auth/**", AgentName: "Alice", Exclusive: true, TTL: time.Hour,
	})
	r := sim.Acquire(AcquireRequest{
		PathPattern: "internal/auth/session.go", AgentName: "Bob", Exclusive: true, TTL: time.Hour,
	})
	if r.Outcome != OutcomeConflict {
		t.Fatalf("Outcome = %s, want conflict", r.Outcome)
	}
	if r.Conflict == nil || r.Conflict.AgentName != "Alice" {
		t.Errorf("Conflict = %+v, want Alice as holder", r.Conflict)
	}
	if !strings.Contains(r.Diagnostic, "Alice") || !strings.Contains(r.Diagnostic, "Bob") {
		t.Errorf("Diagnostic = %q, want both names", r.Diagnostic)
	}
}

func TestAcquire_DisjointPatternsBothAcquired(t *testing.T) {
	t.Parallel()
	sim := NewSimulator(newClockAt(anchor()))
	a := sim.Acquire(AcquireRequest{PathPattern: "internal/auth/**", AgentName: "A", Exclusive: true, TTL: time.Hour})
	b := sim.Acquire(AcquireRequest{PathPattern: "internal/billing/**", AgentName: "B", Exclusive: true, TTL: time.Hour})
	if a.Outcome != OutcomeAcquired || b.Outcome != OutcomeAcquired {
		t.Errorf("Outcomes = %s/%s, want both acquired", a.Outcome, b.Outcome)
	}
	if len(sim.Active()) != 2 {
		t.Errorf("Active = %d, want 2", len(sim.Active()))
	}
}

func TestAcquire_SharedAndSharedCoexist(t *testing.T) {
	t.Parallel()
	sim := NewSimulator(newClockAt(anchor()))
	a := sim.Acquire(AcquireRequest{PathPattern: "docs/**", AgentName: "A", Exclusive: false, TTL: time.Hour})
	b := sim.Acquire(AcquireRequest{PathPattern: "docs/**", AgentName: "B", Exclusive: false, TTL: time.Hour})
	if a.Outcome != OutcomeAcquired {
		t.Errorf("first Outcome = %s, want acquired", a.Outcome)
	}
	if b.Outcome != OutcomeShared {
		t.Errorf("second Outcome = %s, want shared", b.Outcome)
	}
	if b.Conflict == nil || b.Conflict.AgentName != "A" {
		t.Errorf("shared response did not surface co-holder: %+v", b.Conflict)
	}
}

func TestAcquire_ExclusiveOverlappingSharedConflicts(t *testing.T) {
	t.Parallel()
	sim := NewSimulator(newClockAt(anchor()))
	sim.Acquire(AcquireRequest{PathPattern: "docs/**", AgentName: "A", Exclusive: false, TTL: time.Hour})
	r := sim.Acquire(AcquireRequest{PathPattern: "docs/**", AgentName: "B", Exclusive: true, TTL: time.Hour})
	if r.Outcome != OutcomeConflict {
		t.Errorf("Outcome = %s, want conflict (exclusive vs shared)", r.Outcome)
	}
}

func TestAcquire_ExpiredLeaseIsReapedAndReclaimed(t *testing.T) {
	t.Parallel()
	clk := newClockAt(anchor())
	sim := NewSimulator(clk)
	sim.Acquire(AcquireRequest{PathPattern: "internal/x/**", AgentName: "A", Exclusive: true, TTL: 30 * time.Minute})
	clk.Advance(31 * time.Minute) // lease has expired

	r := sim.Acquire(AcquireRequest{PathPattern: "internal/x/**", AgentName: "B", Exclusive: true, TTL: time.Hour})
	if r.Outcome != OutcomeExpiredReclaimed {
		t.Fatalf("Outcome = %s, want expired_reclaimed", r.Outcome)
	}
	if r.Lease == nil || r.Lease.AgentName != "B" {
		t.Errorf("Lease = %+v, want B as new holder", r.Lease)
	}
	active := sim.Active()
	if len(active) != 1 {
		t.Errorf("Active = %d, want 1 (old lease reaped)", len(active))
	}
}

// bd-zshpx: an unrelated stale lease that ages out during another
// pattern's acquire MUST NOT promote that acquire to
// OutcomeExpiredReclaimed. Only a reaped lease whose pattern
// overlapped the new acquire's pattern justifies that outcome.
func TestAcquire_UnrelatedStaleLeaseDoesNotPromoteToExpiredReclaimed(t *testing.T) {
	t.Parallel()
	clk := newClockAt(anchor())
	sim := NewSimulator(clk)

	// Stale lease in an UNRELATED namespace ("x/**"); the new acquire
	// targets a disjoint namespace ("a/b/**").
	sim.Acquire(AcquireRequest{
		PathPattern: "x/**", AgentName: "Stale", Exclusive: true, TTL: 30 * time.Minute,
	})
	clk.Advance(31 * time.Minute) // x/** lease has expired

	r := sim.Acquire(AcquireRequest{
		PathPattern: "a/b/**", AgentName: "Fresh", Exclusive: true, TTL: time.Hour,
	})
	if r.Outcome != OutcomeAcquired {
		t.Fatalf("Outcome = %s, want acquired (the reaped lease was unrelated to a/b/**)", r.Outcome)
	}
	if r.Lease == nil || r.Lease.AgentName != "Fresh" {
		t.Errorf("Lease = %+v, want Fresh as new holder", r.Lease)
	}
	// And the reaper still ran — only one active lease should remain.
	if active := sim.Active(); len(active) != 1 {
		t.Errorf("Active = %d, want 1 (stale x/** reaped, fresh a/b/** alive)", len(active))
	}
}

// Companion to the bd-zshpx fix: when a stale lease overlapping the
// new acquire's pattern is reaped during the same call, the outcome
// must still be OutcomeExpiredReclaimed (the pre-existing semantics
// remain).
func TestAcquire_OverlappingStaleLeaseStillPromotesToExpiredReclaimed(t *testing.T) {
	t.Parallel()
	clk := newClockAt(anchor())
	sim := NewSimulator(clk)

	sim.Acquire(AcquireRequest{
		PathPattern: "internal/auth/**", AgentName: "A", Exclusive: true, TTL: 30 * time.Minute,
	})
	clk.Advance(31 * time.Minute)

	// Different but OVERLAPPING pattern — patternsOverlap should agree
	// because internal/auth/** covers internal/auth/session.go.
	r := sim.Acquire(AcquireRequest{
		PathPattern: "internal/auth/session.go", AgentName: "B", Exclusive: true, TTL: time.Hour,
	})
	if r.Outcome != OutcomeExpiredReclaimed {
		t.Fatalf("Outcome = %s, want expired_reclaimed (overlap with reaped lease)", r.Outcome)
	}
}

// Companion: when MULTIPLE stale leases age out during one acquire and
// only ONE of them overlaps the request, the outcome must still be
// expired_reclaimed (the overlap fired) and the unrelated reap must
// not be required for that signal.
func TestAcquire_OverlapAmongMultipleReapsTriggersExpiredReclaimed(t *testing.T) {
	t.Parallel()
	clk := newClockAt(anchor())
	sim := NewSimulator(clk)

	sim.Acquire(AcquireRequest{PathPattern: "x/**", AgentName: "X", Exclusive: true, TTL: 10 * time.Minute})
	sim.Acquire(AcquireRequest{PathPattern: "y/**", AgentName: "Y", Exclusive: true, TTL: 10 * time.Minute})
	clk.Advance(11 * time.Minute) // both leases expired

	r := sim.Acquire(AcquireRequest{PathPattern: "x/**", AgentName: "Z", Exclusive: true, TTL: time.Hour})
	if r.Outcome != OutcomeExpiredReclaimed {
		t.Fatalf("Outcome = %s, want expired_reclaimed (x/** reaped overlapped the new x/** acquire)", r.Outcome)
	}
}

func TestRelease_ByIDAndByAgent(t *testing.T) {
	t.Parallel()
	sim := NewSimulator(newClockAt(anchor()))
	a := sim.Acquire(AcquireRequest{PathPattern: "x/**", AgentName: "A", Exclusive: true, TTL: time.Hour}).Lease
	sim.Acquire(AcquireRequest{PathPattern: "y/**", AgentName: "B", Exclusive: true, TTL: time.Hour})

	if !sim.Release(a.ID) {
		t.Errorf("Release(%d) returned false", a.ID)
	}
	if sim.Release(9999) {
		t.Errorf("Release of non-existent lease returned true")
	}
	if removed := sim.ReleaseByAgent("B"); removed != 1 {
		t.Errorf("ReleaseByAgent(B) = %d, want 1", removed)
	}
	if len(sim.Active()) != 0 {
		t.Errorf("Active = %d, want 0", len(sim.Active()))
	}
}

func TestActive_DeterministicSort(t *testing.T) {
	t.Parallel()
	sim := NewSimulator(newClockAt(anchor()))
	sim.Acquire(AcquireRequest{PathPattern: "z/**", AgentName: "Z", Exclusive: true, TTL: time.Hour})
	sim.Acquire(AcquireRequest{PathPattern: "a/**", AgentName: "A", Exclusive: true, TTL: time.Hour})
	sim.Acquire(AcquireRequest{PathPattern: "m/**", AgentName: "M", Exclusive: true, TTL: time.Hour})

	first := sim.Active()
	second := sim.Active()
	if len(first) != 3 {
		t.Fatalf("Active = %d, want 3", len(first))
	}
	if first[0].PathPattern != "a/**" || first[2].PathPattern != "z/**" {
		t.Errorf("Active not sorted by pattern: %+v", first)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Errorf("Active output drifted: %s vs %s", a, b)
	}
}

func TestDiagnostics_ListsExclusiveHints(t *testing.T) {
	t.Parallel()
	clk := newClockAt(anchor())
	sim := NewSimulator(clk)
	sim.Acquire(AcquireRequest{PathPattern: "internal/auth/**", AgentName: "Alice", Exclusive: true, TTL: time.Hour, Reason: "bd-3v1gs.5"})
	sim.Acquire(AcquireRequest{PathPattern: "docs/**", AgentName: "Carol", Exclusive: false, TTL: time.Hour})

	rep := sim.Diagnostics()
	if rep.ActiveCount != 2 {
		t.Errorf("ActiveCount = %d, want 2", rep.ActiveCount)
	}
	if len(rep.ConflictHints) != 1 {
		t.Errorf("ConflictHints = %d, want 1 (only exclusive lease produces hint)", len(rep.ConflictHints))
	}
	if !strings.Contains(rep.ConflictHints[0], "internal/auth/**") {
		t.Errorf("ConflictHint missing pattern: %v", rep.ConflictHints)
	}
}

func TestDiagnostics_JSONShapeIsStable(t *testing.T) {
	t.Parallel()
	clk := newClockAt(anchor())
	sim := NewSimulator(clk)
	sim.Acquire(AcquireRequest{PathPattern: "x/**", AgentName: "A", Exclusive: true, TTL: time.Hour})

	a, _ := json.Marshal(sim.Diagnostics())
	b, _ := json.Marshal(sim.Diagnostics())
	if string(a) != string(b) {
		t.Errorf("Diagnostics JSON drifted: %s vs %s", a, b)
	}
	for _, want := range []string{`"generated_at"`, `"active_count"`, `"leases"`, `"conflict_hints"`} {
		if !strings.Contains(string(a), want) {
			t.Errorf("JSON missing %s: %s", want, a)
		}
	}
}

func TestPatternsOverlap_GlobMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want bool
	}{
		{"a/**", "a/b/c.go", true},
		{"a/b/c.go", "a/**", true},
		{"a/**", "b/**", false},
		{"a/**", "a/**", true},
		{"a/*", "a/b", true},
		{"a/*", "a/b/c", false},
		{"*.go", "foo.go", true},
		{"foo.go", "*.go", true},
		{"foo.go", "bar.go", false},
		{"a/**", "a", true},
		{"", "a/**", false},

		// bd-6286k: bare "**" must be a catch-all just like "/**".
		// Pre-fix, patternsOverlap("**", "foo/bar.go") returned false
		// because HasSuffix("**", "/**") is false and path.Match's `*`
		// cannot cross `/`.
		{"**", "foo/bar.go", true},
		{"**", "deep/nested/file.go", true},
		{"**", "anyfile.go", true},
		{"foo/bar.go", "**", true},  // symmetric
		{"/**", "foo/bar.go", true}, // already worked — pin it
	}
	for _, c := range cases {
		got := patternsOverlap(c.a, c.b)
		if got != c.want {
			t.Errorf("patternsOverlap(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSimulator_FullLifecycleScenario(t *testing.T) {
	t.Parallel()
	clk := newClockAt(anchor())
	sim := NewSimulator(clk)

	// Alice grabs auth/** for an hour.
	first := sim.Acquire(AcquireRequest{PathPattern: "internal/auth/**", AgentName: "Alice", Exclusive: true, TTL: time.Hour})
	if first.Outcome != OutcomeAcquired {
		t.Fatalf("first acquire = %s", first.Outcome)
	}

	// Bob tries auth/session.go a minute later — must conflict with diag.
	clk.Advance(1 * time.Minute)
	second := sim.Acquire(AcquireRequest{PathPattern: "internal/auth/session.go", AgentName: "Bob", Exclusive: true, TTL: time.Hour})
	if second.Outcome != OutcomeConflict {
		t.Fatalf("second acquire = %s, want conflict", second.Outcome)
	}

	// Alice releases. Bob retries — must succeed now.
	if !sim.Release(first.Lease.ID) {
		t.Fatalf("release Alice lease failed")
	}
	third := sim.Acquire(AcquireRequest{PathPattern: "internal/auth/session.go", AgentName: "Bob", Exclusive: true, TTL: time.Hour})
	if third.Outcome != OutcomeAcquired {
		t.Fatalf("third acquire = %s, want acquired", third.Outcome)
	}

	// Carol takes a non-overlapping shared lease — must succeed.
	fourth := sim.Acquire(AcquireRequest{PathPattern: "docs/**", AgentName: "Carol", Exclusive: false, TTL: time.Hour})
	if fourth.Outcome != OutcomeAcquired {
		t.Fatalf("fourth acquire = %s, want acquired", fourth.Outcome)
	}

	// Skip 30 minutes — both Bob and Carol still hold their hour-TTL
	// leases. Diagnostics must list both.
	clk.Advance(30 * time.Minute)
	rep := sim.Diagnostics()
	if rep.ActiveCount != 2 {
		t.Errorf("after 30m, ActiveCount = %d, want 2; rep=%+v", rep.ActiveCount, rep.Leases)
	}

	// Skip past both TTLs — every lease has now expired.
	clk.Advance(40 * time.Minute) // total elapsed 70m past Bob+Carol acquire
	rep = sim.Diagnostics()
	if rep.ActiveCount != 0 {
		t.Errorf("after expiry, ActiveCount = %d, want 0; rep=%+v", rep.ActiveCount, rep.Leases)
	}
}
