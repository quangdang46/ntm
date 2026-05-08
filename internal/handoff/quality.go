package handoff

import (
	"math"
	"sort"
	"strings"
	"time"
)

const (
	QualityStatusHigh   = "high"
	QualityStatusMedium = "medium"
	QualityStatusLow    = "low"
)

// QualityReport scores whether a receiving agent can act on a compacted handoff.
type QualityReport struct {
	Score      int                     `yaml:"score" json:"score"`
	Status     string                  `yaml:"status" json:"status"`
	ScoredAt   time.Time               `yaml:"scored_at" json:"scored_at"`
	Dimensions []QualityDimensionScore `yaml:"dimensions" json:"dimensions"`
	Reasons    []string                `yaml:"reasons,omitempty" json:"reasons,omitempty"`
	Hints      []string                `yaml:"hints,omitempty" json:"hints,omitempty"`
}

// QualityDimensionScore is a single deterministic scoring dimension.
type QualityDimensionScore struct {
	Name    string   `yaml:"name" json:"name"`
	Score   int      `yaml:"score" json:"score"`
	Reasons []string `yaml:"reasons,omitempty" json:"reasons,omitempty"`
	Hints   []string `yaml:"hints,omitempty" json:"hints,omitempty"`
}

// UpdateQuality computes and stores the handoff quality report.
func (h *Handoff) UpdateQuality(now time.Time) *Handoff {
	report := h.ScoreQuality(now)
	h.Quality = &report
	return h
}

// ScoreQuality returns a deterministic handoff quality report.
func (h *Handoff) ScoreQuality(now time.Time) QualityReport {
	if now.IsZero() {
		now = time.Now()
	}
	dimensions := []QualityDimensionScore{
		h.scoreCoverage(),
		h.scoreRecency(now),
		h.scoreActionability(),
		h.scoreSourceTraceability(),
	}

	total := 0
	for _, dimension := range dimensions {
		total += dimension.Score
	}
	mean := float64(total) / float64(len(dimensions))
	score := clampQuality(int(math.Round(mean)))
	report := QualityReport{
		Score:      score,
		Status:     qualityStatus(score),
		ScoredAt:   now.UTC(),
		Dimensions: dimensions,
	}

	reasonSet := make(map[string]struct{})
	hintSet := make(map[string]struct{})
	for _, dimension := range dimensions {
		for _, reason := range dimension.Reasons {
			reasonSet[reason] = struct{}{}
		}
		for _, hint := range dimension.Hints {
			hintSet[hint] = struct{}{}
		}
	}
	report.Reasons = sortedQualityKeys(reasonSet)
	report.Hints = sortedQualityKeys(hintSet)
	return report
}

func (h *Handoff) scoreCoverage() QualityDimensionScore {
	score := 0
	var reasons, hints []string

	if strings.TrimSpace(h.Goal) != "" {
		score += 20
	} else {
		reasons = append(reasons, "coverage.missing_goal")
		hints = append(hints, "State what this session accomplished.")
	}
	if strings.TrimSpace(h.Now) != "" {
		score += 20
	} else {
		reasons = append(reasons, "coverage.missing_now")
		hints = append(hints, "State the first action the next agent should take.")
	}
	if strings.TrimSpace(h.Test) != "" {
		score += 15
	} else {
		reasons = append(reasons, "coverage.missing_test")
		hints = append(hints, "Include the exact verification command or proof artifact.")
	}
	if len(h.DoneThisSession) > 0 {
		score += 15
	} else {
		reasons = append(reasons, "coverage.no_completed_tasks")
		hints = append(hints, "List completed tasks with touched files when possible.")
	}
	if h.HasChanges() {
		score += 10
	} else {
		reasons = append(reasons, "coverage.no_file_changes")
	}
	if len(h.Decisions)+len(h.Findings) > 0 {
		score += 10
	} else {
		reasons = append(reasons, "coverage.no_decisions_or_findings")
	}
	if len(h.Next)+len(h.Blockers)+len(h.Questions) > 0 {
		score += 10
	} else {
		reasons = append(reasons, "coverage.no_followup_context")
	}

	return QualityDimensionScore{
		Name:    "coverage",
		Score:   clampQuality(score),
		Reasons: reasons,
		Hints:   uniqueSortedStrings(hints),
	}
}

func (h *Handoff) scoreRecency(now time.Time) QualityDimensionScore {
	updated := h.UpdatedAt
	if updated.IsZero() {
		updated = h.CreatedAt
	}
	if updated.IsZero() {
		return QualityDimensionScore{
			Name:    "recency",
			Score:   35,
			Reasons: []string{"recency.missing_timestamp"},
			Hints:   []string{"Populate created_at or updated_at before handoff."},
		}
	}

	age := now.Sub(updated)
	if age < 0 {
		return QualityDimensionScore{
			Name:    "recency",
			Score:   85,
			Reasons: []string{"recency.future_timestamp"},
			Hints:   []string{"Check clock skew between agents."},
		}
	}
	switch {
	case age <= time.Hour:
		return QualityDimensionScore{Name: "recency", Score: 100}
	case age <= 24*time.Hour:
		return QualityDimensionScore{Name: "recency", Score: 85, Reasons: []string{"recency.same_day"}}
	case age <= 7*24*time.Hour:
		return QualityDimensionScore{
			Name:    "recency",
			Score:   60,
			Reasons: []string{"recency.stale_days"},
			Hints:   []string{"Refresh live bead, mail, and git state before acting."},
		}
	default:
		return QualityDimensionScore{
			Name:    "recency",
			Score:   25,
			Reasons: []string{"recency.stale_week_plus"},
			Hints:   []string{"Treat this handoff as historical context until refreshed."},
		}
	}
}

func (h *Handoff) scoreActionability() QualityDimensionScore {
	score := 0
	var reasons, hints []string

	if strings.TrimSpace(h.Now) != "" {
		score += 30
	} else {
		reasons = append(reasons, "actionability.missing_first_step")
	}
	if len(h.Next) > 0 {
		score += 20
	} else {
		reasons = append(reasons, "actionability.no_next_steps")
		hints = append(hints, "Add a short ordered next-step list.")
	}
	if strings.TrimSpace(h.Test) != "" {
		score += 20
	} else {
		reasons = append(reasons, "actionability.no_verification")
		hints = append(hints, "Include the command or artifact that proves the current state.")
	}
	if len(h.ActiveBeads) > 0 {
		score += 15
	} else {
		reasons = append(reasons, "actionability.no_active_beads")
	}
	if len(h.Blockers) > 0 || h.Status != StatusBlocked {
		score += 15
	} else {
		reasons = append(reasons, "actionability.blocked_without_blockers")
		hints = append(hints, "Name the blocker and the expected unblock condition.")
	}

	return QualityDimensionScore{
		Name:    "actionability",
		Score:   clampQuality(score),
		Reasons: reasons,
		Hints:   uniqueSortedStrings(hints),
	}
}

func (h *Handoff) scoreSourceTraceability() QualityDimensionScore {
	score := 0
	var reasons, hints []string

	if h.HasChanges() {
		score += 25
	} else {
		reasons = append(reasons, "traceability.no_files")
	}
	if len(h.ActiveBeads) > 0 {
		score += 20
	} else {
		reasons = append(reasons, "traceability.no_beads")
	}
	if len(h.AgentMailThreads) > 0 {
		score += 20
	} else {
		reasons = append(reasons, "traceability.no_mail_threads")
	}
	if len(h.CMMemories) > 0 || len(h.Findings) > 0 || len(h.Decisions) > 0 {
		score += 20
	} else {
		reasons = append(reasons, "traceability.no_source_notes")
	}
	if h.AgentID != "" || h.AgentType != "" || h.PaneID != "" {
		score += 15
	} else {
		reasons = append(reasons, "traceability.no_agent_identity")
		hints = append(hints, "Include agent identity or pane ID for follow-up questions.")
	}

	if score < 60 {
		hints = append(hints, "Add file paths, bead IDs, thread IDs, or source notes before compaction.")
	}

	return QualityDimensionScore{
		Name:    "source_traceability",
		Score:   clampQuality(score),
		Reasons: reasons,
		Hints:   uniqueSortedStrings(hints),
	}
}

func clampQuality(score int) int {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func qualityStatus(score int) string {
	switch {
	case score >= 80:
		return QualityStatusHigh
	case score >= 60:
		return QualityStatusMedium
	default:
		return QualityStatusLow
	}
}

func sortedQualityKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return sortedQualityKeys(set)
}
