package ai

import (
	"errors"
	"math/rand/v2"
	"testing"
	"tictactoe/game"
)

func newSeededRNG(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, seed))
}

func TestNewPlayer_Properties(t *testing.T) {
	p := NewPlayer(game.X, Hard, newSeededRNG(1))
	if p.Symbol() != game.X {
		t.Errorf("expected symbol X, got %s", p.Symbol())
	}
	if p.Difficulty() != Hard {
		t.Errorf("expected Hard difficulty, got %s", p.Difficulty())
	}
}

func TestNewPlayer_OSymbol(t *testing.T) {
	p := NewPlayer(game.O, Easy, newSeededRNG(2))
	if p.Symbol() != game.O {
		t.Errorf("expected symbol O, got %s", p.Symbol())
	}
	if p.Difficulty() != Easy {
		t.Errorf("expected Easy difficulty, got %s", p.Difficulty())
	}
}

func TestChooseMove_GameOverError(t *testing.T) {
	g := game.NewGame()
	// X wins top row.
	moves := [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {0, 2}}
	for _, m := range moves {
		g.Play(m[0], m[1])
	}

	p := NewPlayer(game.O, Hard, newSeededRNG(1))
	_, err := p.ChooseMove(g)
	if err == nil {
		t.Fatal("expected error when game is over")
	}
	var gnpErr *GameNotPlayingError
	if !errors.As(err, &gnpErr) {
		t.Fatalf("expected GameNotPlayingError, got %T", err)
	}
}

func TestChooseMove_Easy_ReturnsValidMove(t *testing.T) {
	g := game.NewGame()
	g.Play(0, 0) // X plays
	// Now it's O's turn.
	p := NewPlayer(game.O, Easy, newSeededRNG(42))
	m, err := p.ChooseMove(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Move must be on an empty cell (not 0,0).
	cell, _ := g.Board().Cell(m.Row, m.Col)
	if cell != game.Empty {
		t.Errorf("AI chose occupied cell (%d, %d)", m.Row, m.Col)
	}
}

func TestChooseMove_Easy_OnlyOneOption(t *testing.T) {
	g := game.NewGame()
	// Build a board with one empty cell and no winner:
	// X O X    move order:
	// X X O    1(X) 2(O) 3(X)
	// O . O    4(O) 5(X) 6(O) 7(X) 8(O) → (2,1) is empty, X's turn
	moves := [][2]int{
		{0, 0}, // X
		{0, 1}, // O
		{0, 2}, // X
		{1, 2}, // O
		{1, 0}, // X
		{2, 0}, // O
		{1, 1}, // X
		{2, 2}, // O
	}
	for _, m := range moves {
		if err := g.Play(m[0], m[1]); err != nil {
			t.Fatalf("setup error at (%d,%d): %v", m[0], m[1], err)
		}
	}
	if g.State() != game.Playing {
		t.Fatalf("expected Playing, got %s", g.State())
	}

	p := NewPlayer(g.CurrentPlayer(), Easy, newSeededRNG(1))
	m, err := p.ChooseMove(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Row != 2 || m.Col != 1 {
		t.Errorf("expected (2,1), got (%d,%d)", m.Row, m.Col)
	}
}

func TestChooseMove_Hard_BlocksOpponentWin(t *testing.T) {
	g := game.NewGame()
	// X at (0,0) and (0,1) — X threatens (0,2).
	// O needs to block.
	g.Play(0, 0) // X
	g.Play(1, 1) // O
	g.Play(0, 1) // X

	p := NewPlayer(game.O, Hard, newSeededRNG(1))
	m, err := p.ChooseMove(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Row != 0 || m.Col != 2 {
		t.Errorf("expected AI to block at (0,2), got (%d,%d)", m.Row, m.Col)
	}
}

func TestChooseMove_Hard_TakesWinningMove(t *testing.T) {
	g := game.NewGame()
	// O has (1,0) and (1,1), needs (1,2) to win.
	g.Play(0, 0) // X
	g.Play(1, 0) // O
	g.Play(2, 2) // X
	g.Play(1, 1) // O
	g.Play(2, 0) // X — now O should take (1,2).

	p := NewPlayer(game.O, Hard, newSeededRNG(1))
	m, err := p.ChooseMove(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Row != 1 || m.Col != 2 {
		t.Errorf("expected AI to win at (1,2), got (%d,%d)", m.Row, m.Col)
	}
}

func TestChooseMove_Hard_NeverLoses(t *testing.T) {
	// Play 50 full games where AI (O) plays Hard vs random X.
	// The AI should never lose.
	rng := newSeededRNG(99)
	for i := 0; i < 50; i++ {
		g := game.NewGame()
		aiPlayer := NewPlayer(game.O, Hard, newSeededRNG(uint64(i)))

		for g.State() == game.Playing {
			if g.CurrentPlayer() == game.X {
				// Random move for X.
				available := emptyCells(g.Board())
				pick := available[rng.IntN(len(available))]
				g.Play(pick.Row, pick.Col)
			} else {
				m, err := aiPlayer.ChooseMove(g)
				if err != nil {
					t.Fatalf("game %d: AI error: %v", i, err)
				}
				if err := g.Play(m.Row, m.Col); err != nil {
					t.Fatalf("game %d: play error: %v", i, err)
				}
			}
		}

		if g.State() == game.XWins {
			t.Fatalf("game %d: Hard AI lost to random player", i)
		}
	}
}

func TestChooseMove_Hard_AsX_NeverLoses(t *testing.T) {
	// Play 50 full games where AI (X) plays Hard vs random O.
	rng := newSeededRNG(77)
	for i := 0; i < 50; i++ {
		g := game.NewGame()
		aiPlayer := NewPlayer(game.X, Hard, newSeededRNG(uint64(i+100)))

		for g.State() == game.Playing {
			if g.CurrentPlayer() == game.X {
				m, err := aiPlayer.ChooseMove(g)
				if err != nil {
					t.Fatalf("game %d: AI error: %v", i, err)
				}
				if err := g.Play(m.Row, m.Col); err != nil {
					t.Fatalf("game %d: play error: %v", i, err)
				}
			} else {
				available := emptyCells(g.Board())
				pick := available[rng.IntN(len(available))]
				g.Play(pick.Row, pick.Col)
			}
		}

		if g.State() == game.OWins {
			t.Fatalf("game %d: Hard AI (X) lost to random player", i)
		}
	}
}

func TestChooseMove_Hard_EmptyBoardIsValid(t *testing.T) {
	g := game.NewGame()
	p := NewPlayer(game.X, Hard, newSeededRNG(1))
	m, err := p.ChooseMove(g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Row < 0 || m.Row > 2 || m.Col < 0 || m.Col > 2 {
		t.Errorf("move out of bounds: (%d,%d)", m.Row, m.Col)
	}
}

func TestChooseMove_DrawGameState(t *testing.T) {
	g := game.NewGame()
	// Play to a draw.
	moves := [][2]int{
		{0, 0}, {0, 1}, {0, 2}, {1, 2},
		{1, 0}, {2, 0}, {1, 1}, {2, 2}, {2, 1},
	}
	for _, m := range moves {
		g.Play(m[0], m[1])
	}
	if g.State() != game.Draw {
		t.Fatalf("expected Draw state, got %s", g.State())
	}

	p := NewPlayer(game.X, Hard, newSeededRNG(1))
	_, err := p.ChooseMove(g)
	if err == nil {
		t.Fatal("expected error when game is drawn")
	}
	var gnpErr *GameNotPlayingError
	if !errors.As(err, &gnpErr) {
		t.Fatalf("expected GameNotPlayingError, got %T", err)
	}
}
