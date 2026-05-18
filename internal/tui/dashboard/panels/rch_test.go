package panels

import (
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/status"
	"github.com/Dicklesworthstone/ntm/internal/tools"
)

func TestRCHPanelViewDisabled(t *testing.T) {
	panel := NewRCHPanel()
	panel.SetSize(60, 12)
	panel.SetData(RCHPanelData{
		Loaded:  true,
		Enabled: false,
	})

	out := status.StripANSI(panel.View())
	if !strings.Contains(out, "RCH disabled") {
		t.Fatalf("expected disabled state, got:\n%s", out)
	}
}

func TestRCHPanelViewWithStatsAndWorkers(t *testing.T) {
	panel := NewRCHPanel()
	panel.SetSize(80, 20)
	panel.SetData(RCHPanelData{
		Loaded:    true,
		Enabled:   true,
		Available: true,
		Status: &tools.RCHStatus{
			Enabled: true,
			Workers: []tools.RCHWorker{
				{Name: "builder-1", Available: true, Healthy: true, Load: 10},
				{Name: "builder-2", Available: true, Healthy: true, Load: 90, Queue: 1, CurrentBuild: "cargo build --release"},
			},
			SessionStats: &tools.RCHSessionStats{
				BuildsTotal:      45,
				BuildsRemote:     30,
				BuildsLocal:      15,
				TimeSavedSeconds: 1200,
			},
		},
	})

	out := status.StripANSI(panel.View())
	for _, want := range []string{
		"RCH Build Offload",
		"45 builds",
		"Remote:",
		"Local",
		"Time saved",
		"builder-1",
		"builder-2",
		"cargo build --release",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRCHPanelCurrentBuildWorkerIsBusy(t *testing.T) {
	status := rchWorkerStatus(tools.RCHWorker{
		Name:         "builder-1",
		Available:    true,
		Healthy:      true,
		Load:         10,
		CurrentBuild: "go test ./...",
	})
	if status != "busy" {
		t.Fatalf("expected worker with current build to be busy, got %q", status)
	}
}
