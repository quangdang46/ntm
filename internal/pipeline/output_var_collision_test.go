package pipeline

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestValidateParallelDuplicateOutputVarWarns(t *testing.T) {
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "parallel-output-var-warning",
		Steps: []Step{
			{
				ID: "fanout",
				Parallel: ParallelSpec{Steps: []Step{
					{ID: "left", Prompt: "left", OutputVar: "shared"},
					{ID: "right", Prompt: "right", OutputVar: "shared"},
				}},
			},
		},
	}

	result := Validate(workflow)
	if !result.Valid {
		t.Fatalf("Validate() errors = %+v", result.Errors)
	}
	assertValidationWarning(t, result, "share output_var")
	assertValidationWarning(t, result, "output_var_mode=aggregate")
}

func TestValidateOutputVarModeRejectsUnknownValue(t *testing.T) {
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "bad-output-var-mode",
		Steps: []Step{
			{ID: "bad", Prompt: "bad", OutputVarMode: OutputVarMode("newest")},
		},
	}

	result := Validate(workflow)
	if result.Valid {
		t.Fatal("Validate() succeeded, want invalid output_var_mode error")
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0].Message, "invalid output_var_mode") {
		t.Fatalf("Validate() errors = %+v, want invalid output_var_mode", result.Errors)
	}
}

func TestValidateParallelForeachLastModeWarns(t *testing.T) {
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "foreach-last-warning",
		Steps: []Step{
			{
				ID:            "foreach",
				OutputVar:     "collected",
				OutputVarMode: OutputVarModeLast,
				Foreach: &ForeachConfig{
					Items:    `["a","b"]`,
					Parallel: true,
					Steps: []Step{
						{ID: "body", Prompt: "handle ${item}"},
					},
				},
			},
		},
	}

	result := Validate(workflow)
	if !result.Valid {
		t.Fatalf("Validate() errors = %+v", result.Errors)
	}
	assertValidationWarning(t, result, "parallel foreach")
	assertValidationWarning(t, result, "non-deterministic")
}

func TestExecuteParallelDuplicateOutputVarAggregatesInDeclarationOrder(t *testing.T) {
	e, workflow, step := newOutputVarParallelExecutor(OutputVarModeAggregate)

	result := e.executeParallel(context.Background(), step, workflow)
	if result.Status != StatusCompleted {
		t.Fatalf("executeParallel() status = %s, want completed; error=%+v", result.Status, result.Error)
	}

	got, ok := e.state.Variables["shared"].([]string)
	if !ok {
		t.Fatalf("shared variable type = %T, want []string (%#v)", e.state.Variables["shared"], e.state.Variables["shared"])
	}
	if len(got) != 2 {
		t.Fatalf("len(shared) = %d, want 2: %#v", len(got), got)
	}
	if !strings.Contains(got[0], "left") || !strings.Contains(got[1], "right") {
		t.Fatalf("shared outputs = %#v, want declaration order left/right", got)
	}
}

func TestExecuteParallelDuplicateOutputVarCollectsByStepID(t *testing.T) {
	e, workflow, step := newOutputVarParallelExecutor(OutputVarModeCollect)

	result := e.executeParallel(context.Background(), step, workflow)
	if result.Status != StatusCompleted {
		t.Fatalf("executeParallel() status = %s, want completed; error=%+v", result.Status, result.Error)
	}

	got, ok := e.state.Variables["shared"].(map[string]string)
	if !ok {
		t.Fatalf("shared variable type = %T, want map[string]string (%#v)", e.state.Variables["shared"], e.state.Variables["shared"])
	}
	if !strings.Contains(got["fanout_left"], "left") || !strings.Contains(got["fanout_right"], "right") {
		t.Fatalf("shared outputs = %#v, want entries keyed by scoped step id", got)
	}

	// Regression: the documented nested-variable mechanism must support keyed
	// access into collect-mode outputs (bd-rdzch). Previously navigateNested
	// rejected map[string]string with "cannot access field on type".
	sub := NewSubstitutor(e.state, "", workflow.Name)
	resolved, err := sub.Substitute("${vars.shared.fanout_left}")
	if err != nil {
		t.Fatalf("Substitute(${vars.shared.fanout_left}) error = %v", err)
	}
	if !strings.Contains(resolved, "left") {
		t.Fatalf("Substitute(${vars.shared.fanout_left}) = %q, want substring left", resolved)
	}

	if _, err := sub.Substitute("${vars.shared.missing}"); err == nil {
		t.Fatalf("Substitute(${vars.shared.missing}) succeeded, want missing-key error")
	}
}

func TestStoreParallelOutputVarsAggregatePreservesParsedAlignment(t *testing.T) {
	// bd-iw5bw: aggregate output_var must keep vars.<name> and
	// vars.<name>_parsed positionally aligned. Previously the parsed
	// slice only grew when a child had ParsedData, so a later parsed
	// sibling shifted up the parsed slice and downstream
	// ${vars.shared.N} / ${vars.shared_parsed.N} templates correlated
	// to the wrong child or hit out-of-bounds.
	parent := &Step{
		ID: "fanout",
		Parallel: ParallelSpec{Steps: []Step{
			{ID: "left", OutputVar: "shared"},
			{ID: "middle", OutputVar: "shared"},
			{ID: "right", OutputVar: "shared"},
		}},
	}
	results := []StepResult{
		{StepID: "left", Status: StatusCompleted, Output: "left raw"},
		{StepID: "middle", Status: StatusCompleted, Output: "middle raw", ParsedData: map[string]interface{}{"value": 2}},
		{StepID: "right", Status: StatusCompleted, Output: "right raw", ParsedData: map[string]interface{}{"value": 3}},
	}

	e := &Executor{
		state: &ExecutionState{
			RunID:     "iw5bw-run",
			Variables: make(map[string]interface{}),
		},
	}
	e.storeParallelOutputVars(parent, results, []string{"left", "middle", "right"})

	got, ok := e.state.Variables["shared"].([]string)
	if !ok {
		t.Fatalf("shared = %T, want []string (%#v)", e.state.Variables["shared"], e.state.Variables["shared"])
	}
	if len(got) != 3 || got[0] != "left raw" || got[1] != "middle raw" || got[2] != "right raw" {
		t.Fatalf("shared = %#v, want declaration-order outputs", got)
	}

	parsed, ok := e.state.Variables["shared_parsed"].([]interface{})
	if !ok {
		t.Fatalf("shared_parsed = %T, want []interface{} (%#v)", e.state.Variables["shared_parsed"], e.state.Variables["shared_parsed"])
	}
	if len(parsed) != 3 {
		t.Fatalf("len(shared_parsed) = %d, want 3 to align with shared", len(parsed))
	}
	if parsed[0] != nil {
		t.Fatalf("shared_parsed[0] = %#v, want nil placeholder for unparsed left sibling", parsed[0])
	}
	if m, ok := parsed[1].(map[string]interface{}); !ok || m["value"] != 2 {
		t.Fatalf("shared_parsed[1] = %#v, want middle parsed payload {value: 2}", parsed[1])
	}
	if m, ok := parsed[2].(map[string]interface{}); !ok || m["value"] != 3 {
		t.Fatalf("shared_parsed[2] = %#v, want right parsed payload {value: 3}", parsed[2])
	}
}

// TestStoreParallelOutputVarsAggregatePreservesPositionalIndexing covers
// bd-i3eah: when a substep fails or is cancelled, the aggregate output_var
// slice must reserve a slot rather than shift later siblings up. Pipelines
// that index the slice positionally (${vars.shared.N}) would otherwise
// silently read the wrong row.
func TestStoreParallelOutputVarsAggregatePreservesPositionalIndexing(t *testing.T) {
	parent := &Step{
		ID: "fanout",
		Parallel: ParallelSpec{Steps: []Step{
			{ID: "left", OutputVar: "shared"},
			{ID: "middle", OutputVar: "shared"},
			{ID: "right", OutputVar: "shared"},
		}},
	}
	results := []StepResult{
		{StepID: "left", Status: StatusCompleted, Output: "left raw"},
		{StepID: "middle", Status: StatusFailed, Error: &StepError{Type: "timeout", Message: "boom"}},
		{StepID: "right", Status: StatusCompleted, Output: "right raw", ParsedData: map[string]interface{}{"value": 3}},
	}

	e := &Executor{
		state: &ExecutionState{
			RunID:     "i3eah-run",
			Variables: make(map[string]interface{}),
		},
	}
	e.storeParallelOutputVars(parent, results, []string{"left", "right"})

	got, ok := e.state.Variables["shared"].([]string)
	if !ok {
		t.Fatalf("shared = %T, want []string", e.state.Variables["shared"])
	}
	if len(got) != 3 {
		t.Fatalf("len(shared) = %d, want 3 (one slot per declared substep)", len(got))
	}
	if got[0] != "left raw" {
		t.Errorf("shared[0] = %q, want left raw", got[0])
	}
	if got[1] != "" {
		t.Errorf("shared[1] = %q, want empty placeholder for failed middle sibling", got[1])
	}
	if got[2] != "right raw" {
		t.Errorf("shared[2] = %q, want right raw (must NOT shift up to slot 1)", got[2])
	}

	parsed, ok := e.state.Variables["shared_parsed"].([]interface{})
	if !ok {
		t.Fatalf("shared_parsed = %T, want []interface{}", e.state.Variables["shared_parsed"])
	}
	if len(parsed) != 3 {
		t.Fatalf("len(shared_parsed) = %d, want 3 to align with shared", len(parsed))
	}
	if parsed[1] != nil {
		t.Errorf("shared_parsed[1] = %#v, want nil for failed middle sibling", parsed[1])
	}
	if m, _ := parsed[2].(map[string]interface{}); m == nil || m["value"] != 3 {
		t.Errorf("shared_parsed[2] = %#v, want right parsed payload at index 2", parsed[2])
	}
}

func TestExecuteParallelDuplicateOutputVarLastModeLogsDebug(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	e, workflow, step := newOutputVarParallelExecutor(OutputVarModeLast)
	result := e.executeParallel(context.Background(), step, workflow)
	if result.Status != StatusCompleted {
		t.Fatalf("executeParallel() status = %s, want completed; error=%+v", result.Status, result.Error)
	}
	if _, ok := e.state.Variables["shared"].(string); !ok {
		t.Fatalf("shared variable type = %T, want string", e.state.Variables["shared"])
	}
	if !strings.Contains(buf.String(), "parallel output_var last mode is non-deterministic") {
		t.Fatalf("debug log missing last-mode warning; logs=%s", buf.String())
	}
}

func newOutputVarParallelExecutor(mode OutputVarMode) (*Executor, *Workflow, *Step) {
	parallelSteps := []Step{
		{ID: "left", Prompt: "left", OutputVar: "shared", OutputVarMode: mode},
		{ID: "right", Prompt: "right", OutputVar: "shared"},
	}
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "parallel-output-vars",
		Settings:      DefaultWorkflowSettings(),
		Steps: []Step{
			{ID: "fanout", Parallel: ParallelSpec{Steps: parallelSteps}},
		},
	}
	cfg := DefaultExecutorConfig("test")
	cfg.DryRun = true
	e := NewExecutor(cfg)
	e.graph = NewDependencyGraph(workflow)
	e.state = &ExecutionState{
		RunID:      "test-run",
		WorkflowID: workflow.Name,
		Status:     StatusRunning,
		StartedAt:  time.Now(),
		Steps:      make(map[string]StepResult),
		Variables:  make(map[string]interface{}),
	}
	return e, workflow, &workflow.Steps[0]
}

func assertValidationWarning(t *testing.T, result ValidationResult, want string) {
	t.Helper()
	for _, warning := range result.Warnings {
		if strings.Contains(warning.Message, want) || strings.Contains(warning.Hint, want) {
			return
		}
	}
	t.Fatalf("missing validation warning containing %q; warnings=%+v", want, result.Warnings)
}
