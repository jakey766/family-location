package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"family-location/backend/internal/config"
	"family-location/backend/internal/httpapi"
	"family-location/backend/internal/storage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) > 1 {
		var err error
		switch os.Args[1] {
		case "init-admin":
			err = initAdmin(context.Background(), os.Args[2:], os.Stdin, os.Stdout)
		case "--help", "-h":
			fmt.Fprintln(os.Stdout, "用法: family-location [init-admin [--username NAME --family NAME] [--password-stdin]]")
		default:
			err = fmt.Errorf("未知子命令；使用 --help 查看用法")
		}
		if err != nil {
			logger.Error("command failed", "error", err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	db, err := storage.Open(initCtx, cfg.DBPath)
	cancel()
	if err != nil {
		return err
	}
	defer db.Close()

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	server := &http.Server{
		Handler:           httpapi.New(db),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	logger.Info("server started", "address", listener.Addr().String())
	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		_ = server.Close()
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		logger.Info("server stopping")
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shutdown HTTP: %w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	logger.Info("server stopped")
	return nil
}
