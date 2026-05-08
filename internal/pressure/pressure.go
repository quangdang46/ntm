// Package pressure implements the NTM swarm pressure governor: a small,
// pluggable model that observes machine and swarm pressure (CPU, memory,
// process count, tmux pane activity, pipeline fan-out, rch queue/build
// slot pressure) and gates non-urgent actions when a budget is exceeded.
//
// The package is deliberately self-contained: providers are injected so
// tests run without gopsutil and runtime callers can plug in real probes
// later. See bd-2mb03.1 for design context and acceptance criteria.
package pressure

import (
	"sort"
	"time"
)

// Source identifies a pressure source.
type Source string

const (
	SourceCPU            Source = "cpu"
	SourceMemory         Source = "memory"
	SourceLoad           Source = "load"
	SourceProcCount      Source = "proc_count"
	SourcePaneActivity   Source = "pane_activity"
	SourcePipelineFanout Source = "pipeline_fanout"
	SourceRchQueue       Source = "rch_queue"
	SourceLocalBuild     Source = "local_build"
)

// Level classifies how loaded a pressure source is. Levels are ordered
// from least to most loaded; numerically larger means more loaded so
// callers can compare with `<` / `>` and use math.Max-style reductions.
type Level int

const (
	LevelLow Level = iota
	LevelNormal
	LevelElevated
	LevelHigh
	LevelCritical
)

// String renders a Level as a stable robot-JSON token.
func (l Level) String() string {
	switch l {
	case LevelLow:
		return "low"
	case LevelNormal:
		return "normal"
	case LevelElevated:
		return "elevated"
	case LevelHigh:
		return "high"
	case LevelCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// Reading is a single observation from a Provider at a moment in time.
type Reading struct {
	Source Source  `json:"source"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit,omitempty"`
}

// Thresholds defines the value boundaries between Levels for one source.
// Boundaries are inclusive at the lower end: v >= Elevated => LevelElevated.
// All three values must be non-decreasing; Validate enforces that.
type Thresholds struct {
	Elevated float64 `json:"elevated"`
	High     float64 `json:"high"`
	Critical float64 `json:"critical"`
}

// DefaultThresholds returns the per-source thresholds shipped with NTM.
// These are tuned for a 64-core / 256GB host running a busy swarm; tests
// and config layers may override them per-source.
func DefaultThresholds() map[Source]Thresholds {
	return map[Source]Thresholds{
		SourceCPU:            {Elevated: 0.60, High: 0.80, Critical: 0.92},
		SourceMemory:         {Elevated: 0.65, High: 0.82, Critical: 0.92},
		SourceLoad:           {Elevated: 0.75, High: 1.00, Critical: 1.50},
		SourceProcCount:      {Elevated: 0.70, High: 0.85, Critical: 0.95},
		SourcePaneActivity:   {Elevated: 50, High: 100, Critical: 200},
		SourcePipelineFanout: {Elevated: 16, High: 32, Critical: 64},
		SourceRchQueue:       {Elevated: 0.60, High: 0.80, Critical: 0.95},
		SourceLocalBuild:     {Elevated: 4, High: 8, Critical: 16},
	}
}

// Classify reduces a raw reading value to a Level given thresholds.
// A value below Elevated is split between Low and Normal at Elevated/2.
func Classify(v float64, t Thresholds) Level {
	switch {
	case v >= t.Critical:
		return LevelCritical
	case v >= t.High:
		return LevelHigh
	case v >= t.Elevated:
		return LevelElevated
	case v >= t.Elevated/2:
		return LevelNormal
	default:
		return LevelLow
	}
}

// Snapshot is the aggregated pressure view at a point in time.
type Snapshot struct {
	TakenAt  time.Time         `json:"taken_at"`
	Readings []Reading         `json:"readings"`
	Levels   map[Source]Level  `json:"-"`
	Overall  Level             `json:"-"`
	Limiting []Source          `json:"-"`
}

// limitingSources returns the sources whose Level matches Overall, sorted
// alphabetically so the slice is deterministic for robot output.
func limitingSources(levels map[Source]Level, overall Level) []Source {
	if len(levels) == 0 {
		return nil
	}
	out := make([]Source, 0, len(levels))
	for src, lvl := range levels {
		if lvl == overall {
			out = append(out, src)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// buildSnapshot folds readings + thresholds into a Snapshot.
func buildSnapshot(now time.Time, readings []Reading, thresh map[Source]Thresholds) Snapshot {
	levels := make(map[Source]Level, len(readings))
	overall := LevelLow
	for _, r := range readings {
		t, ok := thresh[r.Source]
		if !ok {
			levels[r.Source] = LevelLow
			continue
		}
		lvl := Classify(r.Value, t)
		levels[r.Source] = lvl
		if lvl > overall {
			overall = lvl
		}
	}
	return Snapshot{
		TakenAt:  now,
		Readings: append([]Reading(nil), readings...),
		Levels:   levels,
		Overall:  overall,
		Limiting: limitingSources(levels, overall),
	}
}
