// Command studycontainer is an explicit container-only entry point. It binds
// port 8080 inside the private container network; figureserver stays loopback-only.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"edsger.local/figureserver/internal/studyhost"
)

func main() {
	handler, err := studyhost.New(os.Getenv("FIGURE_TOKEN"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid_origin_credential")
		os.Exit(1)
	}
	server := &http.Server{
		Addr: "0.0.0.0:8080", Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 16 << 10,
		// The net/http diagnostic logger can contain request bytes. Fixed process
		// status codes below are sufficient; never log educational content.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	stopped := make(chan error, 1)
	go func() { stopped <- server.ListenAndServe() }()
	select {
	case err := <-stopped:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "study_container_unavailable")
			os.Exit(1)
		}
	case <-ctx.Done():
		drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if server.Shutdown(drain) != nil {
			_ = server.Close()
		}
	}
}
