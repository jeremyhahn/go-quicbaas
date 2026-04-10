package game

import "strings"

// Cell represents the contents of a single board position.
type Cell byte

const (
	// Empty represents an unoccupied cell.
	Empty Cell = 0
	// X represents the X player's symbol.
	X Cell = 'X'
	// O represents the O player's symbol.
	O Cell = 'O'
)

// String returns the display character for a cell.
func (c Cell) String() string {
	switch c {
	case X:
		return "X"
	case O:
		return "O"
	default:
		return "."
	}
}

// Symbol is an alias for Cell, used to represent a player's marker.
type Symbol = Cell

// Board represents a 3x3 Tic Tac Toe board stored entirely on the stack.
type Board struct {
	cells [3][3]Cell
}

// NewBoard returns an initialized empty board.
func NewBoard() Board {
	return Board{}
}

// Cell returns the value at the given position.
// Returns an InvalidPositionError if row or col is outside [0, 2].
func (b *Board) Cell(row, col int) (Cell, error) {
	if err := validatePosition(row, col); err != nil {
		return Empty, err
	}
	return b.cells[row][col], nil
}

// SetCell places the given symbol at the specified position.
// Returns an error if:
//   - the position is out of bounds (InvalidPositionError)
//   - the cell is already occupied (CellOccupiedError)
func (b *Board) SetCell(row, col int, symbol Symbol) error {
	if err := validatePosition(row, col); err != nil {
		return err
	}
	if b.cells[row][col] != Empty {
		return &CellOccupiedError{Row: row, Col: col, Symbol: b.cells[row][col]}
	}
	b.cells[row][col] = symbol
	return nil
}

// ClearCell sets the cell at (row, col) back to Empty.
// Returns an InvalidPositionError if the position is out of bounds.
func (b *Board) ClearCell(row, col int) error {
	if err := validatePosition(row, col); err != nil {
		return err
	}
	b.cells[row][col] = Empty
	return nil
}

// IsFull returns true when every cell on the board is occupied.
func (b *Board) IsFull() bool {
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			if b.cells[r][c] == Empty {
				return false
			}
		}
	}
	return true
}

// Reset clears the board, setting every cell back to Empty.
func (b *Board) Reset() {
	b.cells = [3][3]Cell{}
}

// String returns a visual representation of the board.
//
// Example output for an in-progress game:
//
//	X . O
//	. X .
//	O . X
func (b *Board) String() string {
	var sb strings.Builder
	// Pre-grow: 3 rows * (5 cell chars + 2 spaces + newline) is plenty.
	sb.Grow(24)
	for r := 0; r < 3; r++ {
		if r > 0 {
			sb.WriteByte('\n')
		}
		for c := 0; c < 3; c++ {
			if c > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(b.cells[r][c].String())
		}
	}
	return sb.String()
}

// validatePosition checks whether row and col are within [0, 2].
func validatePosition(row, col int) error {
	if row < 0 || row > 2 || col < 0 || col > 2 {
		return &InvalidPositionError{Row: row, Col: col}
	}
	return nil
}
