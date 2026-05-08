package pressure

import (
	"sort"
	"time"
)

// RobotPressure is the JSON-stable surface for `--robot-pressure` and
// any other consumer that wants a snapshot without depending on the
// internal Snapshot/Level types.
type RobotPressure struct {
	Success           bool          `json:"success"`
	Timestamp         string        `json:"timestamp"`
	Mode              string        `json:"mode"`
	Overall           string        `json:"overall"`
	Limiting          []string      `json:"limiting"`
	RecommendedAction string        `json:"recommended_action"`
	Enforcing         bool          `json:"enforcing"`
	Sources           []RobotSource `json:"sources"`
}

// RobotSource is the per-source row inside RobotPressure.
type RobotSource struct {
	Source string  `json:"source"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit,omitempty"`
	Level  string  `json:"level"`
}

// RobotSnapshot returns a stable, deterministic JSON view of the
// governor's most recent snapshot. It does not call Refresh; call
// Refresh first if you want a fresh sample.
func (g *Governor) RobotSnapshot() RobotPressure {
	snap := g.Latest()
	out := RobotPressure{
		Success:   true,
		Timestamp: snap.TakenAt.UTC().Format(time.RFC3339Nano),
		Mode:      string(g.mode),
		Overall:   snap.Overall.String(),
		Limiting:  limitingStrings(snap.Limiting),
		Enforcing: g.mode == ModeEnforce,
	}
	if len(snap.Readings) > 0 {
		// Sort by source name for deterministic ordering.
		ordered := make([]Reading, len(snap.Readings))
		copy(ordered, snap.Readings)
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].Source < ordered[j].Source })
		out.Sources = make([]RobotSource, 0, len(ordered))
		for _, r := range ordered {
			lvl := snap.Levels[r.Source]
			out.Sources = append(out.Sources, RobotSource{
				Source: string(r.Source),
				Value:  r.Value,
				Unit:   r.Unit,
				Level:  lvl.String(),
			})
		}
	}
	out.RecommendedAction = robotRecommendation(snap.Overall)
	return out
}

// robotRecommendation returns a short, stable recommendation token for
// robot output. Keep these strings in sync with the contract tests so
// downstream agents can match on them.
func robotRecommendation(overall Level) string {
	switch overall {
	case LevelCritical:
		return "stop_non_urgent_work"
	case LevelHigh:
		return "defer_non_urgent_work"
	case LevelElevated:
		return "watch_pressure"
	default:
		return "ok"
	}
}
