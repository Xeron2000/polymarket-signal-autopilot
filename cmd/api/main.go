package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"polymarket-signal/internal/connectors/polymarket"
	"polymarket-signal/internal/runtime"
)

func main() {
	addr := getenv("API_ADDR", ":8080")
	gammaURL := getenv("POLY_GAMMA_URL", polymarket.DefaultGammaBaseURL)
	clobURL := getenv("POLY_CLOB_URL", polymarket.DefaultCLOBBaseURL)

	api := runtime.NewAPIServer(
		polymarket.NewGammaRESTClient(gammaURL, nil),
		polymarket.NewCLOBRESTClient(clobURL, nil),
	)

	server := &http.Server{
		Addr:              addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("api listening on %s (gamma=%s, clob=%s)", addr, gammaURL, clobURL)
	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("api server failed: %v", err)
	}
}

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
