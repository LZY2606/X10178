// Command server runs the peak-valley compass HTTP server.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"peakcompass/internal/api"
	"peakcompass/internal/store"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:5238", "listen address")
	dataDir := flag.String("data", "", "project data directory (default ./.peakcompass)")
	noSeed := flag.Bool("no-seed", false, "skip deterministic demo seeding")
	flag.Parse()

	if *dataDir == "" {
		*dataDir = filepath.Join(".", ".peakcompass")
	}
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("data dir: %v", err)
	}
	dsn := "file:" + filepath.Join(*dataDir, "compass.db") + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	st, err := store.Open(dsn)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := api.New(st, *dataDir)

	if !*noSeed {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if _, err := srv.SeedIfEmpty(ctx); err != nil {
			log.Printf("seed: %v", err)
		}
		cancel()
	}

	log.Printf("峰谷罗盘 listening on http://%s (data: %s)", *addr, *dataDir)
	server := &http.Server{
		Addr:              *addr,
		Handler:           srv.Mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
