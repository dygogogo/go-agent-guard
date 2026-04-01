package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"
)

func main() {
	cfg := LoadConfig()

	stdioMode := cfg.MCPTransport == "stdio"
	logger, err := InitLogger("logs", cfg.LogLevel, stdioMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	store, err := NewSQLiteStore(cfg.DBPath)
	if err != nil {
		logger.Fatal("failed to open database", zap.Error(err))
	}
	defer store.Close()

	mcpServer := NewMCPGuardServer(store, cfg, logger)

	switch cfg.MCPTransport {
	case "stdio":
		logger.Info("starting MCP server (stdio transport)")
		if err := server.ServeStdio(mcpServer); err != nil {
			logger.Fatal("stdio server error", zap.Error(err))
		}

	case "sse":
		startHTTPServer(mcpServer, store, cfg, logger, "sse")

	default: // "http" or auto-detected
		startHTTPServer(mcpServer, store, cfg, logger, "streamable-http")
	}
}

func startHTTPServer(mcpServer *server.MCPServer, store BudgetStore, cfg *Config, logger *zap.Logger, transport string) {
	mux := http.NewServeMux()

	switch transport {
	case "sse":
		sseServer := server.NewSSEServer(mcpServer)
		mux.Handle("/sse", sseServer)
		logger.Info("MCP SSE endpoint", zap.String("path", "/sse"))
	default:
		httpServer := server.NewStreamableHTTPServer(mcpServer)
		mux.Handle("/mcp", httpServer)
		logger.Info("MCP StreamableHTTP endpoint", zap.String("path", "/mcp"))
	}

	// Dashboard on same port
	dashboardRouter := NewDashboardRouter(store, cfg, logger)
	mux.Handle("/", dashboardRouter)

	addr := ":" + cfg.DashboardPort
	srv := &http.Server{Addr: addr, Handler: mux}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("HTTP server starting", zap.String("addr", addr), zap.String("transport", transport))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("HTTP server error", zap.Error(err))
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", zap.Error(err))
	}
}
