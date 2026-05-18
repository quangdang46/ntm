package robot

import (
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/tools"
)

func TestRCHWorkerCounts(t *testing.T) {
	workers := []tools.RCHWorker{
		{Name: "a", Available: true, Healthy: true, Load: 10, Queue: 0},
		{Name: "b", Available: true, Healthy: true, Load: 90, Queue: 0},
		{Name: "c", Available: true, Healthy: false, Load: 10, Queue: 0},
		{Name: "d", Available: false, Healthy: true, Load: 10, Queue: 0},
		{Name: "e", Available: true, Healthy: true, Load: 10, Queue: 0, CurrentBuild: "go test ./..."},
	}

	if got := countRCHHealthyWorkers(workers); got != 3 {
		t.Fatalf("expected 3 healthy workers, got %d", got)
	}
	if got := countRCHBusyWorkers(workers); got != 2 {
		t.Fatalf("expected 2 busy workers, got %d", got)
	}
}
