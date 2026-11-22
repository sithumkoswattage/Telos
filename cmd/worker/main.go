// Command worker is a Telos worker node (TEL-80, TEL-38).
//
// It registers with the server, then loops until stopped:
//
//	ask for a task -> count the words -> send the result back
//
// The real work is in internal/worker. Start several workers to process the
// chunks of one job in parallel. Each needs its own name.
//
// Run it (with the server already running) in a second terminal:
//
//	go run ./cmd/worker -name laptop-1
//
// Press Ctrl+C to stop. A task that is in progress is finished first.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"telos/internal/worker"
)

func main() {
	hostname, _ := os.Hostname()
	server := flag.String("server", "http://localhost:8080", "address of the Telos server")
	name := flag.String("name", hostname, "this worker's name (must be unique)")
	flag.Parse()

	// Ctrl+C (or "docker stop") cancels ctx, which tells the worker to stop.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := worker.New(*server, *name).Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
