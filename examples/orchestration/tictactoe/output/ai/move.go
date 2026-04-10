package ai

import "tictactoe/game"

// Move represents a board position chosen by the AI.
type Move struct {
	Row int
	Col int
}

// emptyCells returns all empty positions on the board.
func emptyCells(b *game.Board) []Move {
	moves := make([]Move, 0, 9)
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			cell, _ := b.Cell(r, c)
			if cell == game.Empty {
				moves = append(moves, Move{Row: r, Col: c})
			}
		}
	}
	return moves
}

// opponent returns the other player's symbol.
func opponent(s game.Symbol) game.Symbol {
	if s == game.X {
		return game.O
	}
	return game.X
}
