package panels

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Dicklesworthstone/ntm/internal/tools"
	"github.com/Dicklesworthstone/ntm/internal/tui/components"
	"github.com/Dicklesworthstone/ntm/internal/tui/layout"
	"github.com/Dicklesworthstone/ntm/internal/tui/theme"
)

// RCHPanelData holds the data for the RCH build offload panel.
type RCHPanelData struct {
	Loaded    bool
	Enabled   bool
	Available bool
	Version   string
	Status    *tools.RCHStatus
	Error     error
}

// RCHPanel displays RCH build offload status and worker health.
type RCHPanel struct {
	PanelBase
	data  RCHPanelData
	theme theme.Theme
}

func rchConfig() PanelConfig {
	return PanelConfig{
		ID:              "rch",
		Title:           "RCH Build Offload",
		Priority:        PriorityNormal,
		RefreshInterval: 30 * time.Second,
		MinWidth:        30,
		MinHeight:       6,
		Collapsible:     true,
	}
}

// NewRCHPanel creates a new RCH panel.
func NewRCHPanel() *RCHPanel {
	return &RCHPanel{
		PanelBase: NewPanelBase(rchConfig()),
		theme:     theme.Current(),
	}
}

// Init implements tea.Model.
func (r *RCHPanel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (r *RCHPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return r, nil
}

// SetData updates the panel data.
func (r *RCHPanel) SetData(data RCHPanelData) {
	r.data = data
	if data.Error == nil && data.Loaded {
		r.SetLastUpdate(time.Now())
	}
}

// HasData returns true if the panel has any data (or an error) to display.
func (r *RCHPanel) HasData() bool {
	return r.data.Loaded || r.data.Error != nil
}

// View renders the panel.
func (r *RCHPanel) View() string {
	t := r.theme
	w, h := r.Width(), r.Height()

	if w <= 0 || h <= 0 {
		return ""
	}

	borderColor := t.Surface1
	bgColor := t.Base
	if r.IsFocused() {
		borderColor = t.Primary
		bgColor = t.Surface0
	} else if r.data.Available && r.data.Status != nil {
		if health := rchWorkerHealthSummary(r.data.Status.Workers); health.Total > 0 {
			switch {
			case health.Healthy == 0:
				borderColor = t.Red
			case health.Healthy < health.Total:
				borderColor = t.Yellow
			default:
				borderColor = t.Green
			}
		}
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(bgColor).
		Width(w-2).
		Height(h-2).
		Padding(0, 1)

	var content strings.Builder

	title := r.Config().Title
	if r.data.Error != nil {
		errorBadge := lipgloss.NewStyle().
			Background(t.Red).
			Foreground(t.Base).
			Bold(true).
			Padding(0, 1).
			Render("!")
		title = title + " " + errorBadge
	} else if staleBadge := components.RenderStaleBadge(r.LastUpdate(), r.Config().RefreshInterval); staleBadge != "" {
		title = title + " " + staleBadge
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Lavender).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(t.Surface1).
		Width(w - 4).
		Align(lipgloss.Center)

	content.WriteString(headerStyle.Render(title) + "\n")

	if r.data.Error != nil {
		content.WriteString(components.ErrorState(r.data.Error.Error(), "Waiting for refresh", w-4) + "\n")
		return boxStyle.Render(FitToHeight(content.String(), h-4))
	}

	if !r.data.Loaded {
		content.WriteString("\n" + components.RenderEmptyState(components.EmptyStateOptions{
			Icon:        components.IconWaiting,
			Title:       "Loading RCH status",
			Description: "Checking worker availability",
			Width:       w - 4,
			Centered:    true,
		}))
		return boxStyle.Render(FitToHeight(content.String(), h-4))
	}

	if !r.data.Enabled {
		content.WriteString("\n" + components.RenderEmptyState(components.EmptyStateOptions{
			Icon:        components.IconExternal,
			Title:       "RCH disabled",
			Description: "Enable integrations.rch in config",
			Width:       w - 4,
			Centered:    true,
		}))
		return boxStyle.Render(FitToHeight(content.String(), h-4))
	}

	if !r.data.Available {
		desc := "Install rch to enable build offloading"
		if r.data.Version != "" {
			desc = fmt.Sprintf("RCH %s is unavailable", r.data.Version)
		}
		content.WriteString("\n" + components.RenderEmptyState(components.EmptyStateOptions{
			Icon:        components.IconWaiting,
			Title:       "RCH not available",
			Description: desc,
			Width:       w - 4,
			Centered:    true,
		}))
		return boxStyle.Render(FitToHeight(content.String(), h-4))
	}

	stats := rchStatsFromStatus(r.data.Status)
	health := rchWorkerHealthSummary(nil)
	if r.data.Status != nil {
		health = rchWorkerHealthSummary(r.data.Status.Workers)
	}

	expanded := h >= 18

	content.WriteString("\n")

	statusDot := lipgloss.NewStyle().Foreground(rchOverallColor(t, health)).Render("●")
	summaryText := "No builds yet"
	if stats.Available {
		summaryText = fmt.Sprintf("%d builds (%d remote)", stats.Total, stats.Remote)
		if stats.Derived {
			summaryText += "*"
		}
	}
	content.WriteString(fmt.Sprintf("%s %s\n", statusDot, summaryText))

	timeSaved := formatTimeSaved(stats.TimeSavedSeconds)
	content.WriteString(fmt.Sprintf("Time saved: %s\n", timeSaved))

	if expanded {
		content.WriteString("\n")
		content.WriteString(rchWorkersSummaryLine(health, w-4) + "\n")
		if workersLine := rchWorkersLine(t, r.data.Status, w-4); workersLine != "" {
			content.WriteString(workersLine + "\n")
		}
		if currentLine := rchCurrentBuildLine(r.data.Status, w-4); currentLine != "" {
			content.WriteString(currentLine + "\n")
		}

		remotePct, localPct := rchPercentages(stats)
		note := ""
		if !stats.Available {
			note = " (no data)"
		}
		content.WriteString("\nSession Stats:\n")
		content.WriteString(fmt.Sprintf("  Total builds:     %d%s\n", stats.Total, note))
		content.WriteString(fmt.Sprintf("  Remote:           %d (%d%%)\n", stats.Remote, remotePct))
		content.WriteString(fmt.Sprintf("  Local (fallback): %d (%d%%)\n", stats.Local, localPct))
		content.WriteString(fmt.Sprintf("  Time saved:       %s\n", timeSaved))
	}

	if footer := components.RenderFreshnessFooter(components.FreshnessOptions{
		LastUpdate:      r.LastUpdate(),
		RefreshInterval: r.Config().RefreshInterval,
		Width:           w - 4,
	}); footer != "" {
		content.WriteString(footer + "\n")
	}

	return boxStyle.Render(FitToHeight(content.String(), h-4))
}

type rchStats struct {
	Total            int
	Remote           int
	Local            int
	TimeSavedSeconds int
	Available        bool
	Derived          bool
}

func rchStatsFromStatus(status *tools.RCHStatus) rchStats {
	if status == nil {
		return rchStats{}
	}
	if status.SessionStats != nil {
		stats := status.SessionStats
		available := stats.BuildsTotal > 0 || stats.BuildsRemote > 0 || stats.BuildsLocal > 0 || stats.TimeSavedSeconds > 0
		return rchStats{
			Total:            stats.BuildsTotal,
			Remote:           stats.BuildsRemote,
			Local:            stats.BuildsLocal,
			TimeSavedSeconds: stats.TimeSavedSeconds,
			Available:        available,
		}
	}

	remote := 0
	for _, worker := range status.Workers {
		if worker.BuildsCompleted > 0 {
			remote += worker.BuildsCompleted
		}
	}
	if remote > 0 {
		return rchStats{
			Total:     remote,
			Remote:    remote,
			Local:     0,
			Available: true,
			Derived:   true,
		}
	}
	return rchStats{}
}

type rchWorkerHealth struct {
	Total   int
	Healthy int
	Busy    int
}

func rchWorkerHealthSummary(workers []tools.RCHWorker) rchWorkerHealth {
	summary := rchWorkerHealth{Total: len(workers)}
	for _, w := range workers {
		if !w.Available {
			continue
		}
		if w.Healthy {
			summary.Healthy++
		}
		if rchWorkerStatus(w) == "busy" {
			summary.Busy++
		}
	}
	return summary
}

func rchWorkerStatus(worker tools.RCHWorker) string {
	if !worker.Available {
		return "unavailable"
	}
	if !worker.Healthy {
		return "unhealthy"
	}
	if strings.TrimSpace(worker.CurrentBuild) != "" || worker.Queue > 0 || worker.Load >= 80 {
		return "busy"
	}
	return "healthy"
}

func rchOverallColor(t theme.Theme, summary rchWorkerHealth) lipgloss.Color {
	if summary.Total == 0 {
		return t.Subtext
	}
	switch {
	case summary.Healthy == 0:
		return t.Red
	case summary.Healthy < summary.Total:
		return t.Yellow
	default:
		return t.Green
	}
}

func rchWorkersSummaryLine(summary rchWorkerHealth, width int) string {
	if summary.Total == 0 {
		return "Worker Health: none"
	}
	if width < 0 {
		width = 0
	}
	line := fmt.Sprintf("Worker Health: %d total • %d healthy • %d busy", summary.Total, summary.Healthy, summary.Busy)
	return layout.TruncateWidthDefault(line, width)
}

func rchWorkersLine(t theme.Theme, status *tools.RCHStatus, width int) string {
	if status == nil || len(status.Workers) == 0 {
		return ""
	}
	if width < 0 {
		width = 0
	}
	tokens := make([]string, 0, len(status.Workers))
	for _, worker := range status.Workers {
		icon := rchWorkerIcon(t, worker)
		tokens = append(tokens, fmt.Sprintf("%s %s", icon, worker.Name))
	}
	line := "Workers: " + strings.Join(tokens, "  ")
	return layout.TruncateWidthDefault(line, width)
}

func rchWorkerIcon(t theme.Theme, worker tools.RCHWorker) string {
	color := t.Subtext
	switch rchWorkerStatus(worker) {
	case "healthy":
		color = t.Green
	case "busy":
		color = t.Yellow
	case "unhealthy":
		color = t.Red
	case "unavailable":
		color = t.Overlay
	}
	return lipgloss.NewStyle().Foreground(color).Render("●")
}

func rchCurrentBuildLine(status *tools.RCHStatus, width int) string {
	if status == nil {
		return ""
	}
	if width < 0 {
		width = 0
	}
	for _, worker := range status.Workers {
		if strings.TrimSpace(worker.CurrentBuild) == "" {
			continue
		}
		line := fmt.Sprintf("Current: %s on %s", worker.CurrentBuild, worker.Name)
		return layout.TruncateWidthDefault(line, width)
	}
	return ""
}

func rchPercentages(stats rchStats) (int, int) {
	if stats.Total <= 0 {
		return 0, 0
	}
	remote := int(math.Round(float64(stats.Remote) / float64(stats.Total) * 100))
	local := int(math.Round(float64(stats.Local) / float64(stats.Total) * 100))
	return remote, local
}

func formatTimeSaved(seconds int) string {
	if seconds <= 0 {
		return "N/A"
	}
	d := time.Duration(seconds) * time.Second
	if d < time.Minute {
		return fmt.Sprintf("%ds", seconds)
	}
	if d < time.Hour {
		return fmt.Sprintf("~%dm", int(math.Round(d.Minutes())))
	}
	hours := int(d.Hours())
	minutes := int(math.Round(d.Minutes())) % 60
	if minutes == 0 {
		return fmt.Sprintf("~%dh", hours)
	}
	return fmt.Sprintf("~%dh%dm", hours, minutes)
}
