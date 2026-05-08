package pipeline

import (
	"sync"
	"testing"
	"time"
)

// TestLockOrder_StateMuVarMuCanonicalOrder is the bd-8wo27 regression test.
//
// Before the fix, applyStartFrom acquired stateMu→varMu (canonical) while
// resolveForeachMaxRounds acquired varMu→stateMu (inverted) — a classic
// AB-BA pair. Under -race + concurrent execution the two patterns
// deadlock the goroutines inside sync.RWMutex's writer-starvation guard:
//
//	goroutine A: holds stateMu.Lock(), waiting for varMu.Lock()
//	goroutine B: holds varMu.RLock(), waiting for stateMu.RLock()
//
// After the fix both call sites use stateMu before varMu, so a
// concurrent writer + many concurrent readers run cleanly.
//
// We do not invoke applyStartFrom from multiple goroutines because its
// graph.MarkExecuted side-effect was never designed to be re-entrant
// (separate concurrency contract). Instead we exercise a synthetic
// stateMu.Lock + varMu.Lock writer that *mirrors* applyStartFrom's
// lock pattern verbatim, while resolveForeachMaxRounds runs in parallel
// across the other goroutines. That isolates the lock-ordering question
// from the graph-mutation question.
//
// Run with `go test -race -run TestLockOrder` to also catch any
// remaining unsynchronized access to e.state.
func TestLockOrder_StateMuVarMuCanonicalOrder(t *testing.T) {
	t.Parallel()

	workflow := &Workflow{
		Name: "lock-order",
		Steps: []Step{
			{
				ID: "fanout",
				Foreach: &ForeachConfig{
					Items:     `["a","b"]`,
					MaxRounds: IntOrExpr{Expr: "${vars.rounds}"},
					Steps:     []Step{{ID: "fanout-body"}},
				},
			},
		},
	}

	cfg := DefaultExecutorConfig("session")
	executor := NewExecutor(cfg)
	executor.state = &ExecutionState{
		RunID:      "lock-order-run",
		WorkflowID: workflow.Name,
		Status:     StatusRunning,
		Steps:      map[string]StepResult{},
		Variables:  map[string]interface{}{"rounds": "3"},
	}
	executor.graph = NewDependencyGraph(workflow)
	executor.defaults = workflow.Defaults
	executor.limits = workflow.Settings.Limits.EffectiveLimits()

	const iterations = 500
	const writers = 4 // mimicking applyStartFrom's stateMu→varMu pattern
	const readers = 8 // resolveForeachMaxRounds (now stateMu→varMu too)

	timeout := time.AfterFunc(15*time.Second, func() {
		t.Errorf("bd-8wo27 regression: lock-order test deadlocked; goroutines did not finish within 15s")
	})
	defer timeout.Stop()

	var wg sync.WaitGroup

	// Writers: acquire stateMu.Lock + varMu.Lock the way applyStartFrom
	// does, mutate one entry from each protected map, then release in
	// reverse. Pre-fix, this pattern interleaved with readers below
	// produces an AB-BA deadlock the race detector spots within ~ms.
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := "writer-" + intToString(id) // shared helper in foreach_max_rounds_test.go
			for j := 0; j < iterations; j++ {
				executor.stateMu.Lock()
				executor.varMu.Lock()
				executor.state.Steps[key] = StepResult{StepID: key, Status: StatusCompleted}
				executor.state.Variables[key] = j
				executor.varMu.Unlock()
				executor.stateMu.Unlock()
			}
		}(w)
	}

	// Readers: resolveForeachMaxRounds (now stateMu→varMu after the
	// bd-8wo27 fix). Pre-fix this used varMu→stateMu and would deadlock
	// against the writer pattern above.
	parent := &workflow.Steps[0]
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if _, err := executor.resolveForeachMaxRounds(parent); err != nil {
					t.Errorf("resolveForeachMaxRounds: %v", err)
					return
				}
			}
		}()
	}

	wg.Wait()
}

