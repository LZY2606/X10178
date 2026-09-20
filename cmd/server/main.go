// Command server runs the peak-valley compass HTTP service.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"peakvalley/internal/app"
	"peakvalley/internal/webapi"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:5238", "listen address")
	dataDir := flag.String("data", "", "project data directory (default ./.pvdata)")
	flag.Parse()
	if *dataDir == "" {
		*dataDir = filepath.Join(".", ".pvdata")
	}
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("data dir: %v", err)
	}
	a, err := app.New(*dataDir)
	if err != nil {
		log.Fatalf("app: %v", err)
	}
	defer a.Close()
	srv := webapi.New(a)
	fmt.Printf("峰谷罗盘 listening on http://%s (data: %s)\n", *addr, *dataDir)
	log.Fatal(http.ListenAndServe(*addr, srv.Mux))
}
