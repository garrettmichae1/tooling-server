// Command figureserver is a localhost tool service for figure drawing.
// It is not connected to the Edsger phone app or the live model worker.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"edsger.local/figureserver/docs"
	"edsger.local/figureserver/internal/chart"
	"edsger.local/figureserver/internal/config"
	"edsger.local/figureserver/internal/handout"
	"edsger.local/figureserver/internal/httpapi"
	"edsger.local/figureserver/internal/model"
	"edsger.local/figureserver/internal/session"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg, err := config.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	var mgr *session.Manager
	var sheets *handout.Compiler
	var graphs *chart.Compiler
	var objects *model.Compiler
	if !cfg.ToolsOnly {
		// Child processes run in private directories, so executable paths must
		// be absolute even when the operator uses the relative defaults.
		for _, path := range []*string{&cfg.VectorCraft, &cfg.Typst, &cfg.OpenSCAD} {
			resolved, err := filepath.Abs(*path)
			if err != nil {
				fmt.Fprintln(os.Stderr, "could not resolve renderer path")
				os.Exit(1)
			}
			*path = resolved
		}
		if err := checkBinary(cfg.VectorCraft, "vectorcraft-cli", "VECTORCRAFT_BIN must be named vectorcraft-cli", "vectorcraft-cli is missing or not executable; run scripts/fetch-vectorcraft.sh"); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		if err := checkBinary(cfg.Typst, "typst", "TYPST_BIN must be named typst", "typst is missing or not executable; run scripts/fetch-typst.sh"); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		if err := checkBinary(cfg.OpenSCAD, "openscad", "OPENSCAD_BIN must be named openscad", "openscad is missing or not executable; run scripts/fetch-openscad.sh"); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		mgr, err = session.NewManager(session.Config{
			NewCommand: func() *exec.Cmd {
				return exec.Command(cfg.VectorCraft, "mcp", "--headless")
			},
			MaxSessions: cfg.MaxSessions,
			MaxCalls:    cfg.MaxCalls,
			SessionTTL:  cfg.SessionTTL,
			IdleTTL:     cfg.IdleTTL,
			MaxPNG:      cfg.MaxPNG,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "renderer unavailable")
			os.Exit(1)
		}
		sheets = &handout.Compiler{Bin: cfg.Typst, MaxPDF: cfg.MaxPDF}
		graphs = &chart.Compiler{Bin: cfg.Typst, MaxPDF: cfg.MaxPDF}
		objects = &model.Compiler{Bin: cfg.OpenSCAD}
	}
	closeRenderers := func() {
		if mgr != nil {
			mgr.Close()
		}
	}
	handler := httpapi.New(cfg.Token, cfg.MaxBody, cfg.RatePerMinute, mgr, sheets, graphs, objects, docs.Transcription, log)
	srv := &http.Server{
		Addr:              cfg.Bind,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("listen", "addr", cfg.Bind)
		errCh <- srv.ListenAndServe()
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		closeRenderers()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "server stopped")
			os.Exit(1)
		}
	case <-sig:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		closeRenderers()
	}
}

func checkBinary(path, base, nameErr, missingErr string) error {
	if filepath.Base(path) != base {
		return errors.New(nameErr)
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return errors.New(missingErr)
	}
	return nil
}
