package game

import (
	"errors"
	"testing"
)

func TestNewBoard_AllEmpty(t *testing.T) {
	b := NewBoard()
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			cell, err := b.Cell(r, c)
			if err != nil {
				t.Fatalf("unexpected error at (%d,%d): %v", r, c, err)
			}
			if cell != Empty {
				t.Errorf("expected Empty at (%d,%d), got %s", r, c, cell)
			}
		}
	}
}

func TestBoard_Cell_ValidPositions(t *testing.T) {
	b := NewBoard()
	_ = b.SetCell(0, 0, X)
	_ = b.SetCell(2, 2, O)

	tests := []struct {
		name     string
		row, col int
		want     Cell
	}{
		{"top-left X", 0, 0, X},
		{"bottom-right O", 2, 2, O},
		{"center empty", 1, 1, Empty},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := b.Cell(tc.row, tc.col)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestBoard_Cell_InvalidPositions(t *testing.T) {
	b := NewBoard()
	tests := []struct {
		name     string
		row, col int
	}{
		{"negative row", -1, 0},
		{"negative col", 0, -1},
		{"row too large", 3, 0},
		{"col too large", 0, 3},
		{"both negative", -1, -1},
		{"both too large", 3, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := b.Cell(tc.row, tc.col)
			if err == nil {
				t.Fatal("expected error for out-of-bounds position")
			}
			var posErr *InvalidPositionError
			if !errors.As(err, &posErr) {
				t.Fatalf("expected InvalidPositionError, got %T", err)
			}
		})
	}
}

func TestBoard_SetCell_Success(t *testing.T) {
	b := NewBoard()
	if err := b.SetCell(1, 1, X); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := b.Cell(1, 1)
	if got != X {
		t.Errorf("got %s, want X", got)
	}
}

func TestBoard_SetCell_InvalidPosition(t *testing.T) {
	b := NewBoard()
	err := b.SetCell(-1, 0, X)
	if err == nil {
		t.Fatal("expected error for invalid position")
	}
	var posErr *InvalidPositionError
	if !errors.As(err, &posErr) {
		t.Fatalf("expected InvalidPositionError, got %T", err)
	}
}

func TestBoard_SetCell_Occupied(t *testing.T) {
	b := NewBoard()
	_ = b.SetCell(0, 0, X)
	err := b.SetCell(0, 0, O)
	if err == nil {
		t.Fatal("expected error for occupied cell")
	}
	var occErr *CellOccupiedError
	if !errors.As(err, &occErr) {
		t.Fatalf("expected CellOccupiedError, got %T", err)
	}
	if occErr.Symbol != X {
		t.Errorf("expected occupying symbol X, got %s", occErr.Symbol)
	}
}

func TestBoard_IsFull_EmptyBoard(t *testing.T) {
	b := NewBoard()
	if b.IsFull() {
		t.Error("empty board should not be full")
	}
}

func TestBoard_IsFull_FullBoard(t *testing.T) {
	b := NewBoard()
	symbols := [3][3]Cell{
		{X, O, X},
		{O, X, O},
		{O, X, O},
	}
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			_ = b.SetCell(r, c, symbols[r][c])
		}
	}
	if !b.IsFull() {
		t.Error("board should be full")
	}
}

func TestBoard_IsFull_PartialBoard(t *testing.T) {
	b := NewBoard()
	_ = b.SetCell(0, 0, X)
	_ = b.SetCell(1, 1, O)
	if b.IsFull() {
		t.Error("partially filled board should not be full")
	}
}

func TestBoard_Reset(t *testing.T) {
	b := NewBoard()
	_ = b.SetCell(0, 0, X)
	_ = b.SetCell(1, 1, O)
	b.Reset()
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			cell, _ := b.Cell(r, c)
			if cell != Empty {
				t.Errorf("expected Empty at (%d,%d) after reset, got %s", r, c, cell)
			}
		}
	}
}

func TestBoard_Reset_AfterFull(t *testing.T) {
	b := NewBoard()
	symbols := [3][3]Cell{
		{X, O, X},
		{O, X, O},
		{O, X, O},
	}
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			_ = b.SetCell(r, c, symbols[r][c])
		}
	}
	b.Reset()
	if b.IsFull() {
		t.Error("board should not be full after reset")
	}
}

func TestBoard_String_EmptyBoard(t *testing.T) {
	b := NewBoard()
	want := ". . .\n. . .\n. . ."
	got := b.String()
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBoard_String_WithSymbols(t *testing.T) {
	b := NewBoard()
	_ = b.SetCell(0, 0, X)
	_ = b.SetCell(1, 1, O)
	_ = b.SetCell(2, 2, X)
	want := "X . .\n. O .\n. . X"
	got := b.String()
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBoard_ClearCell_Success(t *testing.T) {
	b := NewBoard()
	_ = b.SetCell(1, 1, X)
	if err := b.ClearCell(1, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := b.Cell(1, 1)
	if got != Empty {
		t.Errorf("expected Empty after ClearCell, got %s", got)
	}
}

func TestBoard_ClearCell_InvalidPosition(t *testing.T) {
	b := NewBoard()
	err := b.ClearCell(3, 0)
	if err == nil {
		t.Fatal("expected error for invalid position")
	}
	var posErr *InvalidPositionError
	if !errors.As(err, &posErr) {
		t.Fatalf("expected InvalidPositionError, got %T", err)
	}
}

func TestBoard_ClearCell_AlreadyEmpty(t *testing.T) {
	b := NewBoard()
	// Clearing an already-empty cell should succeed silently.
	if err := b.ClearCell(0, 0); err != nil {
		t.Fatalf("unexpected error clearing empty cell: %v", err)
	}
}

func TestCell_String(t *testing.T) {
	tests := []struct {
		cell Cell
		want string
	}{
		{Empty, "."},
		{X, "X"},
		{O, "O"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.cell.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
