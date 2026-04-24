package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"local-agent/internal/app"
	"local-agent/internal/config"
	appLog "local-agent/internal/log"
)

// @title Local Agent API
// @version dev
// @description Single-user local Codex-like Agent API built with Go and CloudWeGo Eino.
// @description External effects always flow through ToolRouter, EffectInference, PolicyEngine, ApprovalCenter, and Executor.
// @BasePath /
// @schemes http
func main() {
	configPath := flag.String("config", "config/agent.yaml", "path to agent config")
	checkConfig := flag.Bool("check-config", false, "validate config and create runtime directories")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	logger := appLog.New()
	if *checkConfig {
		report, err := app.CheckConfig(context.Background(), cfg, logger)
		raw, _ := json.MarshalIndent(report, "", "  ")
		_, _ = os.Stdout.Write(append(raw, '\n'))
		if err != nil {
			log.Fatalf("check config: %v", err)
		}
		return
	}
	if report, err := app.CheckConfig(context.Background(), cfg, logger); err != nil {
		log.Fatalf("check config: %v", err)
	} else {
		logger.Info("startup config verified",
			"knowledge_base", report.Knowledge,
			"provider", report.Provider,
			"docs_route", report.DocsRoute,
		)
	}

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
