package game

// MoveRecord captures a single move in the game's history.
type MoveRecord struct {
	Row    int
	Col    int
	Symbol Symbol
	Turn   int
}

// History tracks the sequence of moves in a game.
type History struct {
	moves []MoveRecord
}

// NewHistory creates an empty move history.
func NewHistory() *History {
	return &History{
		moves: make([]MoveRecord, 0, 9),
	}
}

// Record appends a move to the history.
func (h *History) Record(row, col int, symbol Symbol) {
	h.moves = append(h.moves, MoveRecord{
		Row:    row,
		Col:    col,
		Symbol: symbol,
		Turn:   len(h.moves) + 1,
	})
}

// Moves returns a copy of all recorded moves.
func (h *History) Moves() []MoveRecord {
	out := make([]MoveRecord, len(h.moves))
	copy(out, h.moves)
	return out
}

// Len returns the number of recorded moves.
func (h *History) Len() int {
	return len(h.moves)
}

// Last returns the most recent move. Returns a zero MoveRecord and false
// if the history is empty.
func (h *History) Last() (MoveRecord, bool) {
	if len(h.moves) == 0 {
		return MoveRecord{}, false
	}
	return h.moves[len(h.moves)-1], true
}

// Pop removes and returns the most recent move. Returns a zero MoveRecord
// and false if the history is empty.
func (h *History) Pop() (MoveRecord, bool) {
	if len(h.moves) == 0 {
		return MoveRecord{}, false
	}
	last := h.moves[len(h.moves)-1]
	h.moves = h.moves[:len(h.moves)-1]
	return last, true
}

// Clear removes all recorded moves.
func (h *History) Clear() {
	h.moves = h.moves[:0]
}
