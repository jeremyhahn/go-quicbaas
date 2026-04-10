package game

import (
	"errors"
	"testing"
)

func TestNewGame_InitialState(t *testing.T) {
	g := NewGame()
	if g.CurrentPlayer() != X {
		t.Errorf("expected X to start, got %s", g.CurrentPlayer())
	}
	if g.State() != Playing {
		t.Errorf("expected Playing state, got %s", g.State())
	}
	if g.Board() == nil {
		t.Fatal("board should not be nil")
	}
}

func TestNewGame_BoardIsEmpty(t *testing.T) {
	g := NewGame()
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			cell, err := g.Board().Cell(r, c)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cell != Empty {
				t.Errorf("expected Empty at (%d,%d), got %s", r, c, cell)
			}
		}
	}
}

func TestGame_Play_AlternatesTurns(t *testing.T) {
	g := NewGame()
	if g.CurrentPlayer() != X {
		t.Fatalf("expected X first, got %s", g.CurrentPlayer())
	}
	if err := g.Play(0, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.CurrentPlayer() != O {
		t.Errorf("expected O after X's turn, got %s", g.CurrentPlayer())
	}
	if err := g.Play(1, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.CurrentPlayer() != X {
		t.Errorf("expected X after O's turn, got %s", g.CurrentPlayer())
	}
}

func TestGame_Play_InvalidPosition(t *testing.T) {
	g := NewGame()
	err := g.Play(3, 0)
	if err == nil {
		t.Fatal("expected error for invalid position")
	}
	var posErr *InvalidPositionError
	if !errors.As(err, &posErr) {
		t.Fatalf("expected InvalidPositionError, got %T", err)
	}
}

func TestGame_Play_OccupiedCell(t *testing.T) {
	g := NewGame()
	_ = g.Play(0, 0)
	err := g.Play(0, 0)
	if err == nil {
		t.Fatal("expected error for occupied cell")
	}
	var occErr *CellOccupiedError
	if !errors.As(err, &occErr) {
		t.Fatalf("expected CellOccupiedError, got %T", err)
	}
}

func TestGame_Play_XWins(t *testing.T) {
	g := NewGame()
	// X: (0,0), (0,1), (0,2) -- top row
	// O: (1,0), (1,1)
	moves := []struct{ r, c int }{
		{0, 0}, // X
		{1, 0}, // O
		{0, 1}, // X
		{1, 1}, // O
		{0, 2}, // X wins
	}
	for _, m := range moves {
		if err := g.Play(m.r, m.c); err != nil {
			t.Fatalf("unexpected error at (%d,%d): %v", m.r, m.c, err)
		}
	}
	if g.State() != XWins {
		t.Errorf("expected XWins, got %s", g.State())
	}
}

func TestGame_Play_OWins(t *testing.T) {
	g := NewGame()
	// O wins via column 0
	moves := []struct{ r, c int }{
		{0, 1}, // X
		{0, 0}, // O
		{1, 1}, // X
		{1, 0}, // O
		{2, 2}, // X
		{2, 0}, // O wins
	}
	for _, m := range moves {
		if err := g.Play(m.r, m.c); err != nil {
			t.Fatalf("unexpected error at (%d,%d): %v", m.r, m.c, err)
		}
	}
	if g.State() != OWins {
		t.Errorf("expected OWins, got %s", g.State())
	}
}

func TestGame_Play_Draw(t *testing.T) {
	g := NewGame()
	// X O X
	// X X O
	// O X O  -- no winner
	moves := []struct{ r, c int }{
		{0, 0}, // X
		{0, 1}, // O
		{0, 2}, // X
		{1, 2}, // O
		{1, 0}, // X
		{2, 0}, // O
		{1, 1}, // X
		{2, 2}, // O
		{2, 1}, // X -- draw
	}
	for _, m := range moves {
		if err := g.Play(m.r, m.c); err != nil {
			t.Fatalf("unexpected error at (%d,%d): %v", m.r, m.c, err)
		}
	}
	if g.State() != Draw {
		t.Errorf("expected Draw, got %s", g.State())
	}
}

func TestGame_Play_AfterGameOver(t *testing.T) {
	g := NewGame()
	// X wins top row
	moves := []struct{ r, c int }{
		{0, 0}, {1, 0}, {0, 1}, {1, 1}, {0, 2},
	}
	for _, m := range moves {
		_ = g.Play(m.r, m.c)
	}
	err := g.Play(2, 2)
	if err == nil {
		t.Fatal("expected error when playing after game over")
	}
	var goErr *GameOverError
	if !errors.As(err, &goErr) {
		t.Fatalf("expected GameOverError, got %T", err)
	}
	if goErr.FinalState != XWins {
		t.Errorf("expected final state XWins, got %s", goErr.FinalState)
	}
}

func TestGame_Reset(t *testing.T) {
	g := NewGame()
	_ = g.Play(0, 0)
	_ = g.Play(1, 1)
	g.Reset()

	if g.CurrentPlayer() != X {
		t.Errorf("expected X after reset, got %s", g.CurrentPlayer())
	}
	if g.State() != Playing {
		t.Errorf("expected Playing after reset, got %s", g.State())
	}
	cell, _ := g.Board().Cell(0, 0)
	if cell != Empty {
		t.Errorf("expected Empty at (0,0) after reset, got %s", cell)
	}
}

func TestGame_Reset_AfterWin(t *testing.T) {
	g := NewGame()
	moves := []struct{ r, c int }{
		{0, 0}, {1, 0}, {0, 1}, {1, 1}, {0, 2},
	}
	for _, m := range moves {
		_ = g.Play(m.r, m.c)
	}
	if g.State() != XWins {
		t.Fatalf("expected XWins before reset, got %s", g.State())
	}
	g.Reset()
	if g.State() != Playing {
		t.Errorf("expected Playing after reset, got %s", g.State())
	}
	// Should be able to play again.
	if err := g.Play(0, 0); err != nil {
		t.Fatalf("unexpected error after reset: %v", err)
	}
}

func TestGame_Play_DoesNotSwitchOnWin(t *testing.T) {
	g := NewGame()
	// X wins, current player should remain X (no switch after winning move).
	moves := []struct{ r, c int }{
		{0, 0}, {1, 0}, {0, 1}, {1, 1}, {0, 2},
	}
	for _, m := range moves {
		_ = g.Play(m.r, m.c)
	}
	if g.CurrentPlayer() != X {
		t.Errorf("expected current player to remain X after winning, got %s", g.CurrentPlayer())
	}
}

func TestGame_Board_ReturnsSameBoard(t *testing.T) {
	g := NewGame()
	b1 := g.Board()
	b2 := g.Board()
	if b1 != b2 {
		t.Error("Board() should return the same pointer on repeated calls")
	}
}
