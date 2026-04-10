// Package game provides a complete Tic Tac Toe game engine with board management,
// state tracking, and win detection.
package game

import "fmt"

// InvalidPositionError indicates that a row or column index is out of the valid range [0, 2].
type InvalidPositionError struct {
	Row int
	Col int
}

// Error returns a human-readable description of the invalid position.
func (e *InvalidPositionError) Error() string {
	return fmt.Sprintf("invalid position: row=%d, col=%d (must be 0-2)", e.Row, e.Col)
}

// CellOccupiedError indicates that a move was attempted on a cell that already contains a symbol.
type CellOccupiedError struct {
	Row    int
	Col    int
	Symbol Cell
}

// Error returns a human-readable description of the occupied cell.
func (e *CellOccupiedError) Error() string {
	return fmt.Sprintf("cell (%d, %d) is already occupied by %s", e.Row, e.Col, e.Symbol)
}

// GameOverError indicates that a move was attempted after the game has already ended.
type GameOverError struct {
	FinalState State
}

// Error returns a human-readable description of the game-over condition.
func (e *GameOverError) Error() string {
	return fmt.Sprintf("game is already over: %s", e.FinalState)
}

// UndoNotAllowedError indicates that an undo was attempted when no moves exist.
type UndoNotAllowedError struct{}

// Error returns a human-readable description of the undo failure.
func (e *UndoNotAllowedError) Error() string {
	return "undo not allowed: no moves to undo"
}

// NotYourTurnError indicates a move was attempted by the wrong player.
type NotYourTurnError struct {
	Expected Symbol
	Got      Symbol
}

// Error returns a description of the turn violation.
func (e *NotYourTurnError) Error() string {
	return fmt.Sprintf("not your turn: expected %s, got %s", e.Expected, e.Got)
}
