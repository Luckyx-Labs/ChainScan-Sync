package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Luckyx-Labs/chainscan-sync/internal/config"
	"github.com/gin-gonic/gin"
)

// MockListener mock implementation of ListenerReloader for testing
type MockListener struct {
	chainName   string
	reloadError error
	reloadCount int
}

func NewMockListener(chainName string, reloadError error) *MockListener {
	return &MockListener{
		chainName:   chainName,
		reloadError: reloadError,
	}
}

func (m *MockListener) ReloadMonitorAddresses() error {
	m.reloadCount++
	return m.reloadError
}

func (m *MockListener) GetChainName() string {
	return m.chainName
}

// newTestServer creates a test server
func newTestServer() *Server {
	gin.SetMode(gin.TestMode)
	cfg := &config.APIConfig{
		AuthToken: "test-token",
	}
	engine := gin.New()
	s := &Server{
		config:    cfg,
		engine:    engine,
		listeners: make([]ListenerReloader, 0),
	}
	return s
}

// parseResponseBody parses the response body
type ReloadResponse struct {
	Success   bool         `json:"success"`
	Message   string       `json:"message"`
	Error     string       `json:"error"`
	Results   []ResultItem `json:"results"`
	Timestamp string       `json:"timestamp"`
}

type ResultItem struct {
	Chain   string `json:"chain"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

func TestReloadMonitorAddresses_AllSuccess(t *testing.T) {
	s := newTestServer()

	// Register successful listener
	s.listeners = append(s.listeners, NewMockListener("base-sepolia", nil))

	// Create test request
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/reload-addresses", nil)

	// Call function
	s.reloadMonitorAddresses(c)

	// Verify response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp ReloadResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Success != true {
		t.Errorf("Expected success=true, got %v", resp.Success)
	}
	if resp.Message != "reload completed" {
		t.Errorf("Expected message='reload completed', got '%s'", resp.Message)
	}
	if len(resp.Results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(resp.Results))
	}

	// verify all results are success
	for _, result := range resp.Results {
		if !result.Success {
			t.Errorf("Expected all results to be success, got failure for %s", result.Chain)
		}
	}

	// verify listeners were called
	for _, listener := range s.listeners {
		mock := listener.(*MockListener)
		if mock.reloadCount != 1 {
			t.Errorf("Expected listener %s to be called once, got %d", mock.chainName, mock.reloadCount)
		}
	}
}
