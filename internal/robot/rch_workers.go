// Package robot provides machine-readable output for AI agents.
// rch_workers.go implements the --robot-rch-workers command.
package robot

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/tools"
)

// RCHWorkersOptions configures the --robot-rch-workers command.
type RCHWorkersOptions struct {
	Worker string // filter to a specific worker name
}

// RCHWorkersOutput represents the response from --robot-rch-workers.
type RCHWorkersOutput struct {
	RobotResponse
	Worker  string          `json:"worker,omitempty"`
	Workers []RCHWorkerInfo `json:"workers"`
}

// RCHWorkerInfo represents a worker in robot output.
type RCHWorkerInfo struct {
	Name            string `json:"name"`
	Host            string `json:"host,omitempty"`
	Status          string `json:"status"`
	CurrentBuild    string `json:"current_build,omitempty"`
	BuildsCompleted int    `json:"builds_completed,omitempty"`
	CPUPercent      int    `json:"cpu_percent,omitempty"`
	Load            int    `json:"load,omitempty"`
	Queue           int    `json:"queue,omitempty"`
	LastSeen        string `json:"last_seen,omitempty"`
	Available       bool   `json:"available"`
	Healthy         bool   `json:"healthy"`
}

// GetRCHWorkers returns worker information from rch.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetRCHWorkers(opts RCHWorkersOptions) (*RCHWorkersOutput, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adapter := tools.NewRCHAdapter()
	availability, err := adapter.GetAvailability(ctx)
	if err != nil {
		return &RCHWorkersOutput{
			RobotResponse: NewErrorResponse(err, ErrCodeInternalError, "Failed to check RCH availability"),
			Worker:        opts.Worker,
			Workers:       []RCHWorkerInfo{},
		}, nil
	}

	if availability == nil || !availability.Available {
		return &RCHWorkersOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("rch not installed"),
				ErrCodeDependencyMissing,
				"Install rch and ensure it is on PATH",
			),
			Worker:  opts.Worker,
			Workers: []RCHWorkerInfo{},
		}, nil
	}
	if !availability.Compatible {
		return &RCHWorkersOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("rch version incompatible"),
				ErrCodeDependencyMissing,
				"Update rch to a compatible version",
			),
			Worker:  opts.Worker,
			Workers: []RCHWorkerInfo{},
		}, nil
	}

	workers, err := adapter.GetWorkers(ctx)
	if err != nil {
		return &RCHWorkersOutput{
			RobotResponse: NewErrorResponse(err, ErrCodeInternalError, "Failed to query RCH workers"),
			Worker:        opts.Worker,
			Workers:       []RCHWorkerInfo{},
		}, nil
	}

	if opts.Worker != "" {
		var filtered []tools.RCHWorker
		for _, w := range workers {
			if w.Name == opts.Worker {
				filtered = append(filtered, w)
			}
		}
		if len(filtered) == 0 {
			return &RCHWorkersOutput{
				RobotResponse: NewErrorResponse(
					fmt.Errorf("worker '%s' not found", opts.Worker),
					ErrCodeInvalidFlag,
					"Use --robot-rch-workers to list available workers",
				),
				Worker:  opts.Worker,
				Workers: []RCHWorkerInfo{},
			}, nil
		}
		workers = filtered
	}

	outputWorkers := make([]RCHWorkerInfo, 0, len(workers))
	for _, w := range workers {
		outputWorkers = append(outputWorkers, RCHWorkerInfo{
			Name:            w.Name,
			Host:            w.Host,
			Status:          rchWorkerStatus(w),
			CurrentBuild:    w.CurrentBuild,
			BuildsCompleted: w.BuildsCompleted,
			CPUPercent:      w.CPUPercent,
			Load:            w.Load,
			Queue:           w.Queue,
			LastSeen:        w.LastSeen,
			Available:       w.Available,
			Healthy:         w.Healthy,
		})
	}

	sort.Slice(outputWorkers, func(i, j int) bool {
		return outputWorkers[i].Name < outputWorkers[j].Name
	})

	return &RCHWorkersOutput{
		RobotResponse: NewRobotResponse(true),
		Worker:        opts.Worker,
		Workers:       outputWorkers,
	}, nil
}

// PrintRCHWorkers handles the --robot-rch-workers command.
// This is a thin wrapper around GetRCHWorkers() for CLI output.
func PrintRCHWorkers(opts RCHWorkersOptions) error {
	output, err := GetRCHWorkers(opts)
	if err != nil {
		return err
	}
	return outputJSON(output)
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
