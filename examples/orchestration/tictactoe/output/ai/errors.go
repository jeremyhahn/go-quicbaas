// Package ai provides AI opponents for the Tic Tac Toe game engine.
package ai

import "fmt"

// NoMovesAvailableError indicates the AI was asked to move but no empty cells exist.
type NoMovesAvailableError struct{}

// Error returns a description of the no-moves condition.
func (e *NoMovesAvailableError) Error() string {
	return "no moves available: board is full"
}

// GameNotPlayingError indicates the AI was asked to move but the game is already over.
type GameNotPlayingError struct {
	Reason string
}

// Error returns a description of why the AI cannot move.
func (e *GameNotPlayingError) Error() string {
	return fmt.Sprintf("cannot move: game is not in playing state (%s)", e.Reason)
}
