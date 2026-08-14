package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	intercall "github.com/cerasos/intercall/go"
	"github.com/cerasos/intercall/go/internal/integration/fixtures/e2eexport"
	"github.com/cerasos/intercall/go/internal/integration/fixtures/e2eimport"
	"github.com/cerasos/intercall/go/transport/websocket"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, acceptErr := websocket.AcceptStream(ctx, w, r, nil)
		if acceptErr != nil {
			return
		}
		conn, connectionErr := intercall.NewNegotiatedServerConnection(ctx, stream, e2eexport.ExportBinding(), e2eimport.ImportBinding())
		if connectionErr != nil {
			_ = stream.Close()
			return
		}
		go func() { _ = e2eimport.Ping(intercall.WithConnection(ctx, conn)) }()
		_ = conn.Wait()
	})}
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()
	fmt.Printf("READY ws://%s\n", listener.Addr().String())
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
