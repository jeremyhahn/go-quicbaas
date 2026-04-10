package game

// Game is the top-level orchestrator for a Tic Tac Toe match.
// It manages the board, tracks whose turn it is, enforces game rules,
// and records move history.
type Game struct {
	board   Board
	current Symbol
	state   State
	result  Result
	history *History
}

// NewGame creates a new game with an empty board. X always moves first.
func NewGame() *Game {
	return &Game{
		board:   NewBoard(),
		current: X,
		state:   Playing,
		history: NewHistory(),
	}
}

// Board returns a pointer to the current board.
func (g *Game) Board() *Board {
	return &g.board
}

// CurrentPlayer returns the symbol of the player whose turn it is.
func (g *Game) CurrentPlayer() Symbol {
	return g.current
}

// State returns the current game state.
func (g *Game) State() State {
	return g.state
}

// Result returns the full result including winning line positions.
// Only meaningful when the game is no longer in Playing state.
func (g *Game) Result() Result {
	return g.result
}

// History returns the game's move history.
func (g *Game) History() *History {
	return g.history
}

// Play places the current player's symbol at (row, col), evaluates the
// board for a winner or draw, and switches turns if the game continues.
//
// Returns an error if:
//   - the game is already over (GameOverError)
//   - the position is invalid (InvalidPositionError)
//   - the cell is occupied (CellOccupiedError)
func (g *Game) Play(row, col int) error {
	if g.state != Playing {
		return &GameOverError{FinalState: g.state}
	}
	if err := g.board.SetCell(row, col, g.current); err != nil {
		return err
	}
	g.history.Record(row, col, g.current)
	g.result = CheckWinner(&g.board)
	g.state = g.result.State
	if g.state == Playing {
		g.switchPlayer()
	}
	return nil
}

// Undo reverses the most recent move, restoring the previous board state
// and turn order. Returns the undone move.
//
// Returns UndoNotAllowedError if no moves exist to undo.
func (g *Game) Undo() (MoveRecord, error) {
	rec, ok := g.history.Pop()
	if !ok {
		return MoveRecord{}, &UndoNotAllowedError{}
	}
	g.board.ClearCell(rec.Row, rec.Col)
	g.current = rec.Symbol
	g.result = CheckWinner(&g.board)
	g.state = g.result.State
	return rec, nil
}

// Reset restores the game to its initial state with an empty board
// and X as the starting player.
func (g *Game) Reset() {
	g.board.Reset()
	g.current = X
	g.state = Playing
	g.result = Result{}
	g.history.Clear()
}

// MoveCount returns the number of moves played so far.
func (g *Game) MoveCount() int {
	return g.history.Len()
}

// switchPlayer toggles the current player between X and O.
func (g *Game) switchPlayer() {
	if g.current == X {
		g.current = O
	} else {
		g.current = X
	}
}
