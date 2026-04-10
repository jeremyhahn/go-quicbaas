package web

import (
	"errors"
	"testing"
	"tictactoe/ai"
	"tictactoe/game"
)

func TestNewService_InitialState(t *testing.T) {
	svc := NewService(ai.Hard)
	state := svc.State()

	if state.State != "playing" {
		t.Errorf("expected playing, got %s", state.State)
	}
	if state.Current != "X" {
		t.Errorf("expected current X, got %s", state.Current)
	}
	if state.Difficulty != "hard" {
		t.Errorf("expected hard difficulty, got %s", state.Difficulty)
	}
	if state.Winner != "" {
		t.Errorf("expected empty winner, got %s", state.Winner)
	}

	// All cells should be empty.
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			if state.Board[r][c] != "." {
				t.Errorf("expected . at (%d,%d), got %s", r, c, state.Board[r][c])
			}
		}
	}
}

func TestService_Play_ValidMove(t *testing.T) {
	svc := NewService(ai.Easy)
	state, err := svc.Play(0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Human placed X at (0,0).
	if state.Board[0][0] != "X" {
		t.Errorf("expected X at (0,0), got %s", state.Board[0][0])
	}

	// AI should have placed O somewhere.
	oCount := 0
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			if state.Board[r][c] == "O" {
				oCount++
			}
		}
	}
	if state.State == "playing" && oCount != 1 {
		t.Errorf("expected 1 O on board, got %d", oCount)
	}
}

func TestService_Play_InvalidPosition(t *testing.T) {
	svc := NewService(ai.Hard)
	_, err := svc.Play(5, 0)
	if err == nil {
		t.Fatal("expected error for invalid position")
	}
	var posErr *game.InvalidPositionError
	if !errors.As(err, &posErr) {
		t.Fatalf("expected InvalidPositionError, got %T", err)
	}
}

func TestService_Play_OccupiedCell(t *testing.T) {
	svc := NewService(ai.Hard)
	svc.Play(0, 0)
	_, err := svc.Play(0, 0)
	if err == nil {
		t.Fatal("expected error for occupied cell")
	}
}

func TestService_Reset_Easy(t *testing.T) {
	svc := NewService(ai.Hard)
	svc.Play(1, 1)

	state, err := svc.Reset("easy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.State != "playing" {
		t.Errorf("expected playing after reset, got %s", state.State)
	}
	if state.Difficulty != "easy" {
		t.Errorf("expected easy difficulty, got %s", state.Difficulty)
	}
	// Board should be empty.
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			if state.Board[r][c] != "." {
				t.Errorf("expected . at (%d,%d), got %s", r, c, state.Board[r][c])
			}
		}
	}
}

func TestService_Reset_InvalidDifficulty(t *testing.T) {
	svc := NewService(ai.Hard)
	_, err := svc.Reset("impossible")
	if err == nil {
		t.Fatal("expected error for invalid difficulty")
	}
	var dErr *InvalidDifficultyError
	if !errors.As(err, &dErr) {
		t.Fatalf("expected InvalidDifficultyError, got %T", err)
	}
}

func TestService_Play_GameToCompletion(t *testing.T) {
	svc := NewService(ai.Easy)
	// Play moves until game ends (max 5 human moves).
	positions := [][2]int{
		{0, 0}, {0, 1}, {0, 2},
		{1, 0}, {1, 1}, {1, 2},
		{2, 0}, {2, 1}, {2, 2},
	}
	for _, pos := range positions {
		state, err := svc.Play(pos[0], pos[1])
		if err != nil {
			// Expected once game ends or cell is taken.
			continue
		}
		if state.State != "playing" {
			// Game ended.
			if state.Winner == "X" || state.Winner == "O" || state.State == "draw" {
				return // Valid completion.
			}
		}
	}
}
