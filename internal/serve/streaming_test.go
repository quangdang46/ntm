package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// withTestRequestID adds a test request ID to the context.
func withTestRequestID(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestIDKey, "test-req-id")
}

func TestStreamingStatsEndpoint(t *testing.T) {
	// Create a minimal server with stream manager
	s := &Server{
		wsHub: NewWSHub(),
	}

	// Initialize stream manager
	cfg := tmux.DefaultPaneStreamerConfig()
	cfg.FIFODir = t.TempDir()
	s.streamManager = tmux.NewStreamManager(tmux.DefaultClient, func(event tmux.StreamEvent) {
		// No-op callback for testing
	}, cfg)

	// Create request
	req := httptest.NewRequest("GET", "/api/v1/streaming/stats", nil)
	req = req.WithContext(withTestRequestID(req.Context()))
	w := httptest.NewRecorder()

	// Call handler
	s.handleStreamingStatsV1(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}
	if resp["active_streams"].(float64) != 0 {
		t.Errorf("expected 0 active streams, got %v", resp["active_streams"])
	}
	if resp["flush_interval_ms"].(float64) != 50 {
		t.Errorf("expected flush interval 50ms, got %v", resp["flush_interval_ms"])
	}
}

func TestStartStopStreamEndpointRejectsMissingPane(t *testing.T) {
	// Create a minimal server with stream manager
	s := &Server{
		wsHub: NewWSHub(),
	}

	// Initialize stream manager
	cfg := tmux.DefaultPaneStreamerConfig()
	cfg.FIFODir = t.TempDir()
	s.streamManager = tmux.NewStreamManager(tmux.DefaultClient, func(event tmux.StreamEvent) {
		// No-op callback for testing
	}, cfg)

	// Create router with params
	r := chi.NewRouter()
	r.Post("/sessions/{sessionId}/panes/{paneIdx}/stream", func(w http.ResponseWriter, req *http.Request) {
		req = req.WithContext(withTestRequestID(req.Context()))
		s.handleStartPaneStreamV1(w, req)
	})
	r.Delete("/sessions/{sessionId}/panes/{paneIdx}/stream", func(w http.ResponseWriter, req *http.Request) {
		req = req.WithContext(withTestRequestID(req.Context()))
		s.handleStopPaneStreamV1(w, req)
	})

	// Test start streaming. The HTTP endpoint resolves the route's pane index
	// to a live tmux pane ID before starting a stream, so a missing session
	// should fail without creating a ghost fallback stream.
	req := httptest.NewRequest("POST", "/sessions/testsession/panes/0/stream", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("start stream: expected status 404, got %d: %s", w.Code, w.Body.String())
	}

	var startResp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &startResp); err != nil {
		t.Fatalf("failed to parse start response: %v", err)
	}
	if startResp["success"] != false {
		t.Errorf("expected success=false, got %v", startResp["success"])
	}

	// Verify streaming was not started.
	active := s.streamManager.ListActive()
	if len(active) != 0 {
		t.Errorf("expected active=[], got %v", active)
	}

	// Test stop streaming
	req = httptest.NewRequest("DELETE", "/sessions/testsession/panes/0/stream", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("stop stream: expected status 404, got %d: %s", w.Code, w.Body.String())
	}

	// Verify streaming stopped
	active = s.streamManager.ListActive()
	if len(active) != 0 {
		t.Errorf("expected no active streams, got %v", active)
	}
}

func TestStreamEndpointValidation(t *testing.T) {
	// Create a minimal server with stream manager
	s := &Server{
		wsHub: NewWSHub(),
	}

	cfg := tmux.DefaultPaneStreamerConfig()
	cfg.FIFODir = t.TempDir()
	s.streamManager = tmux.NewStreamManager(tmux.DefaultClient, func(event tmux.StreamEvent) {}, cfg)

	r := chi.NewRouter()
	r.Post("/sessions/{sessionId}/panes/{paneIdx}/stream", func(w http.ResponseWriter, req *http.Request) {
		req = req.WithContext(withTestRequestID(req.Context()))
		s.handleStartPaneStreamV1(w, req)
	})

	// Test invalid pane index
	req := httptest.NewRequest("POST", "/sessions/testsession/panes/invalid/stream", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid pane index, got %d", w.Code)
	}
}

func TestStreamManagerIntegration(t *testing.T) {
	// Test that stream manager correctly formats WebSocket events
	var receivedEvents []tmux.StreamEvent

	cfg := tmux.DefaultPaneStreamerConfig()
	cfg.FIFODir = t.TempDir()
	cfg.FallbackPollInterval = 50 // Fast polling for test

	sm := tmux.NewStreamManager(tmux.DefaultClient, func(event tmux.StreamEvent) {
		receivedEvents = append(receivedEvents, event)
	}, cfg)
	defer sm.StopAll()

	// Start streaming for a nonexistent pane (will use fallback)
	if err := sm.StartStream("fake:0"); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	// Check stats
	stats := sm.Stats()
	if stats["active_streams"].(int) != 1 {
		t.Errorf("expected 1 active stream, got %v", stats["active_streams"])
	}

	// Give fallback mode a moment to initialize
	// (it will fail to capture but won't error)

	// Stop streaming
	sm.StopStream("fake:0")

	// Verify stopped
	if len(sm.ListActive()) != 0 {
		t.Error("expected no active streams after stop")
	}
}

func TestStreamTopicForTarget(t *testing.T) {

	tests := []struct {
		name   string
		target string
		want   string
	}{
		{name: "raw target", target: "demo:2", want: "panes:demo:2"},
		{name: "tmux pane id target", target: "%3", want: "panes:%3"},
		{name: "already canonical", target: "panes:demo:2", want: "panes:demo:2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := streamTopicForTarget(tt.target); got != tt.want {
				t.Fatalf("streamTopicForTarget(%q) = %q, want %q", tt.target, got, tt.want)
			}
		})
	}
}
