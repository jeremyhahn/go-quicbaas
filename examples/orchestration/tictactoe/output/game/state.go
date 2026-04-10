package game

// State represents the current phase of a game.
type State byte

const (
	// Playing means the game is still in progress.
	Playing State = iota
	// XWins means player X has won.
	XWins
	// OWins means player O has won.
	OWins
	// Draw means the board is full with no winner.
	Draw
)

// stateNames maps each State to its display string. Using a map keeps
// the lookup at O(1) and avoids a switch statement.
var stateNames = map[State]string{
	Playing: "playing",
	XWins:   "X wins",
	OWins:   "O wins",
	Draw:    "draw",
}

// String returns a human-readable name for the game state.
func (s State) String() string {
	if name, ok := stateNames[s]; ok {
		return name
	}
	return "unknown"
}

// Position represents a row, column coordinate on the board.
type Position struct {
	Row int
	Col int
}

// Result holds the outcome of evaluating a board, including which cells
// form the winning line (if any).
type Result struct {
	State       State
	WinningLine [3]Position
}

// line describes three board coordinates that form a potential winning line.
type line [3]Position

// lines contains all eight possible winning lines on a 3x3 board.
// Pre-computed once to avoid repeated allocation.
var lines = [8]line{
	// Rows
	{{0, 0}, {0, 1}, {0, 2}},
	{{1, 0}, {1, 1}, {1, 2}},
	{{2, 0}, {2, 1}, {2, 2}},
	// Columns
	{{0, 0}, {1, 0}, {2, 0}},
	{{0, 1}, {1, 1}, {2, 1}},
	{{0, 2}, {1, 2}, {2, 2}},
	// Diagonals
	{{0, 0}, {1, 1}, {2, 2}},
	{{0, 2}, {1, 1}, {2, 0}},
}

// winStateForSymbol maps a player symbol to its corresponding win state.
var winStateForSymbol = map[Cell]State{
	X: XWins,
	O: OWins,
}

// CheckWinner evaluates the board and returns the result.
// It checks all eight possible winning lines, then falls back to
// detecting a draw (full board) or an ongoing game.
func CheckWinner(b *Board) Result {
	for _, l := range lines {
		a := b.cells[l[0].Row][l[0].Col]
		if a == Empty {
			continue
		}
		if a == b.cells[l[1].Row][l[1].Col] && a == b.cells[l[2].Row][l[2].Col] {
			return Result{
				State:       winStateForSymbol[a],
				WinningLine: l,
			}
		}
	}
	if b.IsFull() {
		return Result{State: Draw}
	}
	return Result{State: Playing}
}
