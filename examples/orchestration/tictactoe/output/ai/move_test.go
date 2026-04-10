package ai

import (
	"testing"
	"tictactoe/game"
)

func TestEmptyCells_EmptyBoard(t *testing.T) {
	b := game.NewBoard()
	cells := emptyCells(&b)
	if len(cells) != 9 {
		t.Errorf("expected 9 empty cells, got %d", len(cells))
	}
}

func TestEmptyCells_PartialBoard(t *testing.T) {
	b := game.NewBoard()
	b.SetCell(0, 0, game.X)
	b.SetCell(1, 1, game.O)
	b.SetCell(2, 2, game.X)
	cells := emptyCells(&b)
	if len(cells) != 6 {
		t.Errorf("expected 6 empty cells, got %d", len(cells))
	}
	// Verify none of the returned cells are occupied positions.
	occupied := map[[2]int]bool{
		{0, 0}: true, {1, 1}: true, {2, 2}: true,
	}
	for _, m := range cells {
		if occupied[[2]int{m.Row, m.Col}] {
			t.Errorf("returned occupied cell (%d, %d)", m.Row, m.Col)
		}
	}
}

func TestEmptyCells_FullBoard(t *testing.T) {
	b := game.NewBoard()
	symbols := [9]game.Symbol{
		game.X, game.O, game.X,
		game.X, game.X, game.O,
		game.O, game.X, game.O,
	}
	for i, s := range symbols {
		b.SetCell(i/3, i%3, s)
	}
	cells := emptyCells(&b)
	if len(cells) != 0 {
		t.Errorf("expected 0 empty cells, got %d", len(cells))
	}
}

func TestOpponent(t *testing.T) {
	if opponent(game.X) != game.O {
		t.Error("opponent of X should be O")
	}
	if opponent(game.O) != game.X {
		t.Error("opponent of O should be X")
	}
}
