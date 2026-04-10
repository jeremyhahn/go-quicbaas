package ai

import (
	"math/rand/v2"
	"tictactoe/game"
)

// Player is an AI opponent that can choose moves on a Tic Tac Toe board.
type Player struct {
	symbol     game.Symbol
	difficulty Difficulty
	rng        *rand.Rand
}

// NewPlayer creates an AI player with the given symbol, difficulty, and
// random source. The random source is used for the Easy difficulty and
// for tie-breaking in Hard mode.
func NewPlayer(symbol game.Symbol, difficulty Difficulty, rng *rand.Rand) *Player {
	return &Player{
		symbol:     symbol,
		difficulty: difficulty,
		rng:        rng,
	}
}

// Symbol returns the AI player's symbol.
func (p *Player) Symbol() game.Symbol {
	return p.symbol
}

// Difficulty returns the AI's difficulty level.
func (p *Player) Difficulty() Difficulty {
	return p.difficulty
}

// ChooseMove selects the best move for the AI on the given board.
//
// Returns an error if:
//   - the game is not in playing state (GameNotPlayingError)
//   - the board has no empty cells (NoMovesAvailableError)
func (p *Player) ChooseMove(g *game.Game) (Move, error) {
	if g.State() != game.Playing {
		return Move{}, &GameNotPlayingError{Reason: g.State().String()}
	}

	available := emptyCells(g.Board())
	if len(available) == 0 {
		return Move{}, &NoMovesAvailableError{}
	}

	switch p.difficulty {
	case Easy:
		return p.chooseRandom(available), nil
	default:
		return p.chooseMinimax(g.Board(), available), nil
	}
}

// chooseRandom picks a random empty cell.
func (p *Player) chooseRandom(available []Move) Move {
	return available[p.rng.IntN(len(available))]
}

// chooseMinimax finds the optimal move using the minimax algorithm.
// When multiple moves share the best score, one is chosen at random.
func (p *Player) chooseMinimax(b *game.Board, available []Move) Move {
	bestScore := -2 // worse than any real score
	var bestMoves []Move

	for _, m := range available {
		b.SetCell(m.Row, m.Col, p.symbol)
		score := p.minimax(b, opponent(p.symbol), false)
		b.ClearCell(m.Row, m.Col)

		if score > bestScore {
			bestScore = score
			bestMoves = bestMoves[:0]
			bestMoves = append(bestMoves, m)
		} else if score == bestScore {
			bestMoves = append(bestMoves, m)
		}
	}

	if len(bestMoves) == 1 {
		return bestMoves[0]
	}
	return bestMoves[p.rng.IntN(len(bestMoves))]
}

// minimax recursively evaluates board positions.
// Returns +1 if the maximizing player (AI) wins, -1 if the opponent wins, 0 for draw.
func (p *Player) minimax(b *game.Board, current game.Symbol, isMaximizing bool) int {
	result := game.CheckWinner(b)

	switch result.State {
	case game.XWins:
		if p.symbol == game.X {
			return 1
		}
		return -1
	case game.OWins:
		if p.symbol == game.O {
			return 1
		}
		return -1
	case game.Draw:
		return 0
	}

	available := emptyCells(b)

	if isMaximizing {
		best := -2
		for _, m := range available {
			b.SetCell(m.Row, m.Col, current)
			score := p.minimax(b, opponent(current), false)
			b.ClearCell(m.Row, m.Col)
			if score > best {
				best = score
			}
		}
		return best
	}

	best := 2
	for _, m := range available {
		b.SetCell(m.Row, m.Col, current)
		score := p.minimax(b, opponent(current), true)
		b.ClearCell(m.Row, m.Col)
		if score < best {
			best = score
		}
	}
	return best
}
