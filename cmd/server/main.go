package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"desrosiers.org/brain/internal/server"
	"desrosiers.org/brain/internal/store"
)

func main() {
	godotenv.Load()

	s, err := store.Open(env("BRAIN_DB", "data/brain.db"), env("BRAIN_NOTES", "data/notes"))
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	addr := env("LISTEN_ADDR", ":8080")
	srv := &http.Server{
		Addr:         addr,
		Handler:      server.New(s).Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		certFile, keyFile := env("TLS_CERT", ""), env("TLS_KEY", "")
		if certFile != "" && keyFile != "" {
			// HTTPS with a Tailscale cert (`tailscale cert <hostname>`).
			log.Printf("brain API listening (https) on %s", addr)
			if err := srv.ListenAndServeTLS(certFile, keyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("listen: %v", err)
			}
			return
		}
		log.Printf("brain API listening (http) on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
