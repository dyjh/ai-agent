package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"local-agent/internal/api"
)

// Server is the HTTP application.
type Server struct {
	bootstrap *Bootstrap
	http      *http.Server
}

// NewServer constructs the HTTP server.
func NewServer(bootstrap *Bootstrap) *Server {
	router := api.NewRouter(api.Dependencies{
		Logger:    bootstrap.Logger,
		Config:    bootstrap.Config,
		Store:     bootstrap.Store,
		Runtime:   bootstrap.Runtime,
		Approvals: bootstrap.Approvals,
		Router:    bootstrap.Router,
		Memory:    bootstrap.Memory,
		Knowledge: bootstrap.Knowledge,
		Skills:    bootstrap.Skills,
		MCP:       bootstrap.MCP,
		Ops:       bootstrap.Ops,
	})
	addr := fmt.Sprintf("%s:%d", bootstrap.Config.Server.Host, bootstrap.Config.Server.Port)

	return &Server{
		bootstrap: bootstrap,
		http: &http.Server{
			Addr:              addr,
			Handler:           router,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
}

// Run starts the HTTP server and blocks until ctx is canceled or ListenAndServe returns.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		s.bootstrap.Logger.Info("http server listening", "addr", s.http.Addr)
		err := s.http.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
