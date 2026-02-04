package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Luckyx-Labs/chainscan-sync/internal/config"
	"github.com/Luckyx-Labs/chainscan-sync/internal/manager"
	"github.com/Luckyx-Labs/chainscan-sync/pkg/logger"
)

var (
	configPath = flag.String("config", "config.yaml", "path to config file")
	version    = "1.0.0"
)

func main() {
	flag.Parse()

	fmt.Printf("Contract Listener v%s\n", version)
	fmt.Printf("Config file: %s\n\n", *configPath)

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	if err := logger.InitLogger(cfg.Log); err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	logger.Info("=== Contract Listener Starting ===")
	logger.Infof("Version: %s", version)

	mgr := manager.NewManager(cfg)

	if err := mgr.Initialize(); err != nil {
		logger.Fatalf("Failed to initialize: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		logger.Fatalf("Failed to start: %v", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	logger.Info("Contract listener is running, press Ctrl+C to stop...")

	<-sigChan
	logger.Info("Received stop signal, shutting down...")

	if err := mgr.Stop(); err != nil {
		logger.Errorf("Failed to stop: %v", err)
	}

	logger.Info("=== Contract Listener Stopped ===")
}
