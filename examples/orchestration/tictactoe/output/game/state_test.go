package game

import "testing"

func TestState_String(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{Playing, "playing"},
		{XWins, "X wins"},
		{OWins, "O wins"},
		{Draw, "draw"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.state.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestState_String_Unknown(t *testing.T) {
	unknown := State(255)
	if got := unknown.String(); got != "unknown" {
		t.Errorf("got %q, want %q", got, "unknown")
	}
}

func TestCheckWinner_XWinsRow(t *testing.T) {
	tests := []struct {
		name string
		row  int
	}{
		{"top row", 0},
		{"middle row", 1},
		{"bottom row", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBoard()
			for c := 0; c < 3; c++ {
				_ = b.SetCell(tc.row, c, X)
			}
			result := CheckWinner(&b)
			if result.State != XWins {
				t.Errorf("expected XWins, got %s", result.State)
			}
			for i, pos := range result.WinningLine {
				if pos.Row != tc.row || pos.Col != i {
					t.Errorf("winning line[%d] = (%d,%d), want (%d,%d)", i, pos.Row, pos.Col, tc.row, i)
				}
			}
		})
	}
}

func TestCheckWinner_OWinsColumn(t *testing.T) {
	tests := []struct {
		name string
		col  int
	}{
		{"left column", 0},
		{"center column", 1},
		{"right column", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBoard()
			for r := 0; r < 3; r++ {
				_ = b.SetCell(r, tc.col, O)
			}
			result := CheckWinner(&b)
			if result.State != OWins {
				t.Errorf("expected OWins, got %s", result.State)
			}
			for i, pos := range result.WinningLine {
				if pos.Row != i || pos.Col != tc.col {
					t.Errorf("winning line[%d] = (%d,%d), want (%d,%d)", i, pos.Row, pos.Col, i, tc.col)
				}
			}
		})
	}
}

func TestCheckWinner_DiagonalWins(t *testing.T) {
	tests := []struct {
		name      string
		symbol    Cell
		positions [3]Position
		wantState State
	}{
		{
			"main diagonal X",
			X,
			[3]Position{{0, 0}, {1, 1}, {2, 2}},
			XWins,
		},
		{
			"anti diagonal O",
			O,
			[3]Position{{0, 2}, {1, 1}, {2, 0}},
			OWins,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBoard()
			for _, p := range tc.positions {
				_ = b.SetCell(p.Row, p.Col, tc.symbol)
			}
			result := CheckWinner(&b)
			if result.State != tc.wantState {
				t.Errorf("expected %s, got %s", tc.wantState, result.State)
			}
		})
	}
}

func TestCheckWinner_Draw(t *testing.T) {
	// X O X
	// X X O
	// O X O
	b := NewBoard()
	moves := []struct {
		r, c int
		s    Cell
	}{
		{0, 0, X}, {0, 1, O}, {0, 2, X},
		{1, 0, X}, {1, 1, X}, {1, 2, O},
		{2, 0, O}, {2, 1, X}, {2, 2, O},
	}
	for _, m := range moves {
		_ = b.SetCell(m.r, m.c, m.s)
	}
	result := CheckWinner(&b)
	if result.State != Draw {
		t.Errorf("expected Draw, got %s", result.State)
	}
}

func TestCheckWinner_StillPlaying(t *testing.T) {
	b := NewBoard()
	_ = b.SetCell(0, 0, X)
	result := CheckWinner(&b)
	if result.State != Playing {
		t.Errorf("expected Playing, got %s", result.State)
	}
}

func TestCheckWinner_EmptyBoard(t *testing.T) {
	b := NewBoard()
	result := CheckWinner(&b)
	if result.State != Playing {
		t.Errorf("expected Playing for empty board, got %s", result.State)
	}
}

func TestCheckWinner_WinningLinePositions(t *testing.T) {
	b := NewBoard()
	// Fill main diagonal with X
	_ = b.SetCell(0, 0, X)
	_ = b.SetCell(1, 1, X)
	_ = b.SetCell(2, 2, X)
	result := CheckWinner(&b)
	expected := [3]Position{{0, 0}, {1, 1}, {2, 2}}
	if result.WinningLine != expected {
		t.Errorf("winning line %v, want %v", result.WinningLine, expected)
	}
}

func TestCheckWinner_DrawHasNoWinningLine(t *testing.T) {
	b := NewBoard()
	moves := []struct {
		r, c int
		s    Cell
	}{
		{0, 0, X}, {0, 1, O}, {0, 2, X},
		{1, 0, X}, {1, 1, X}, {1, 2, O},
		{2, 0, O}, {2, 1, X}, {2, 2, O},
	}
	for _, m := range moves {
		_ = b.SetCell(m.r, m.c, m.s)
	}
	result := CheckWinner(&b)
	zeroLine := [3]Position{}
	if result.WinningLine != zeroLine {
		t.Errorf("draw should have zero-value winning line, got %v", result.WinningLine)
	}
}
