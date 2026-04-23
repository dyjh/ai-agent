package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"local-agent/internal/app"
	"local-agent/internal/config"
	appLog "local-agent/internal/log"
)

func main() {
	cfg, err := config.Load("config/agent.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	logger := appLog.New()
	bootstrap, err := app.NewBootstrap(context.Background(), cfg, logger)
	if err != nil {
		log.Fatalf("bootstrap app: %v", err)
	}
	server := app.NewServer(bootstrap)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := server.Run(ctx); err != nil {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}
