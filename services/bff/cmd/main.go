// bff is the thin analyst API in front of case-service.
package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"esm/services/bff/internal/api"

	"esm/libs/go-common/envx"
)

func main() {
	log := envx.Logger("bff")
	caseURL, err := url.Parse(envx.String("CASE_SERVICE_URL", "http://case-service:8002"))
	if err != nil {
		log.Error("invalid CASE_SERVICE_URL", "error", err)
		os.Exit(1)
	}

	analysts, err := api.ParseAnalysts(os.Getenv("ANALYST_TOKENS"))
	if err != nil {
		log.Error("analyst tokens", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              envx.String("LISTEN_ADDR", ":8080"),
		Handler:           api.Handler(caseURL, analysts, log),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("bff listening", "addr", srv.Addr, "case_service", caseURL.String())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("bff listen error", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	log.Info("bff stopped")
}
