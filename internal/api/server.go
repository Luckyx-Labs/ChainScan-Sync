package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Luckyx-Labs/chainscan-sync/internal/config"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
	"github.com/gin-gonic/gin"
)

// ListenerReloader reload interface for listeners that can reload monitor addresses
type ListenerReloader interface {
	// ReloadMonitorAddresses reload monitor addresses
	ReloadMonitorAddresses() error
	// GetChainName get chain name
	GetChainName() string
}

// Server API server
type Server struct {
	config    *config.APIConfig
	engine    *gin.Engine
	server    *http.Server
	listeners []ListenerReloader
}

// NewServer creates a new API server
func NewServer(cfg *config.APIConfig) *Server {
	// Set gin mode
	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(RequestLogger())

	s := &Server{
		config:    cfg,
		engine:    engine,
		listeners: make([]ListenerReloader, 0),
	}

	return s
}

// RegisterListener registers a listener
func (s *Server) RegisterListener(listener ListenerReloader) {
	s.listeners = append(s.listeners, listener)
	logger.Infof("API server registered listener: %s", listener.GetChainName())
}

// SetupRoutes sets up routes
func (s *Server) SetupRoutes() {

	// Authorized API group
	authorized := s.engine.Group("/api/v1")
	authorized.Use(AuthMiddleware(s.config.AuthToken))
	{
		// Reload monitor addresses
		authorized.POST("/reload-addresses", s.reloadMonitorAddresses)
	}
}

// Start starts the API server
func (s *Server) Start() error {
	s.SetupRoutes()

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.engine,
	}

	logger.Infof("API server starting on %s", addr)
	errCh := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
			logger.Errorf("API server error: %v", err)
		}
	}()

	// Briefly wait to confirm startup
	select {
	case err := <-errCh:
		return err
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

// Stop stops the API server
func (s *Server) Stop() error {
	if s.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown API server: %w", err)
	}

	logger.Info("API server stopped")
	return nil
}

// reloadMonitorAddresses reloads monitor addresses
func (s *Server) reloadMonitorAddresses(c *gin.Context) {
	if len(s.listeners) == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "no listeners registered",
		})
		return
	}

	results := make([]gin.H, 0, len(s.listeners))
	hasError := false

	for _, listener := range s.listeners {
		chainName := listener.GetChainName()
		if err := listener.ReloadMonitorAddresses(); err != nil {
			hasError = true
			results = append(results, gin.H{
				"chain":   chainName,
				"success": false,
				"error":   err.Error(),
			})
			logger.Errorf("Failed to reload addresses for %s: %v", chainName, err)
		} else {
			results = append(results, gin.H{
				"chain":   chainName,
				"success": true,
			})
			logger.Infof("Successfully reloaded addresses for %s", chainName)
		}
	}

	status := http.StatusOK
	if hasError {
		status = http.StatusPartialContent
	}

	c.JSON(status, gin.H{
		"success":   !hasError,
		"message":   "reload completed",
		"results":   results,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}
