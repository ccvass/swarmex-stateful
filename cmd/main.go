package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"github.com/ccvass/swarmex/swarmex-stateful"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Error("docker client failed", "error", err)
		os.Exit(1)
	}
	ctrl := stateful.New(cli, logger)

	go func() {
		http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"status":"ok"}`)) })
		http.ListenAndServe(":8080", nil)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		cancel()
	}()

	logger.Info("stateful controller started")
	msgs, errs := cli.Events(ctx, types.EventsOptions{Filters: filters.NewArgs(filters.Arg("type", "service"))})
	for {
		select {
		case event := <-msgs:
			ctrl.HandleEvent(ctx, event)
		case err := <-errs:
			if err != nil {
				logger.Error("event stream error", "error", err)
			}
			return
		case <-ctx.Done():
			return
		}
	}
}
