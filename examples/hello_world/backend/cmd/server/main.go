package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"example.com/intercall-hello/backend/gen/backendexport"
	"example.com/intercall-hello/backend/gen/browserimport"
	"github.com/cerasos/intercall/go/transport/websocket"
)

func main() {
	address := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	staticDir := flag.String("static", "../frontend/dist", "directory containing the built frontend")
	flag.Parse()

	intercallHandler := websocket.NewHandler(
		backendexport.ExportBinding(),
		browserimport.ImportBinding(),
	)
	mux := http.NewServeMux()
	mux.Handle("/intercall", intercallHandler)
	mux.Handle("/", http.FileServer(http.Dir(*staticDir)))

	server := &http.Server{
		Addr:              *address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("hello world available at http://%s", *address)
		serverErrors <- server.ListenAndServe()
	}()

	signalContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
		return
	case <-signalContext.Done():
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := intercallHandler.Shutdown(shutdownContext); err != nil {
		log.Printf("shut down InterCall connections: %v", err)
	}
	if err := server.Shutdown(shutdownContext); err != nil {
		log.Printf("shut down HTTP server: %v", err)
	}
}
