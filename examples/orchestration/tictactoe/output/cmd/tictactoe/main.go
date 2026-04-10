// Command tictactoe starts the Tic Tac Toe web server.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"tictactoe/ai"
	"tictactoe/web"
)

const defaultAddr = ":8080"

func main() {
	addr := defaultAddr
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	svc := web.NewService(ai.Hard)
	handler := web.NewHandler(svc)

	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// Graceful shutdown on interrupt.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	fmt.Printf("Tic Tac Toe server listening on http://localhost%s\n", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
