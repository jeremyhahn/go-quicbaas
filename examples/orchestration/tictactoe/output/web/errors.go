// Package web provides an HTTP server for the Tic Tac Toe game.
package web

import "fmt"

// InvalidDifficultyError indicates an unrecognized difficulty value was provided.
type InvalidDifficultyError struct {
	Value string
}

// Error returns a description of the invalid difficulty.
func (e *InvalidDifficultyError) Error() string {
	return fmt.Sprintf("invalid difficulty: %q (must be \"easy\" or \"hard\")", e.Value)
}

// ServerStartError indicates the HTTP server failed to start.
type ServerStartError struct {
	Addr string
	Err  error
}

// Error returns a description of the server start failure.
func (e *ServerStartError) Error() string {
	return fmt.Sprintf("failed to start server on %s: %v", e.Addr, e.Err)
}

// Unwrap returns the underlying error.
func (e *ServerStartError) Unwrap() error {
	return e.Err
}
