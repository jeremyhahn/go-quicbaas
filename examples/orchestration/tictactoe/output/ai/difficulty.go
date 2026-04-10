package ai

// Difficulty controls the AI's playing strength.
type Difficulty byte

const (
	// Easy picks a random empty cell.
	Easy Difficulty = iota
	// Hard plays optimally using the minimax algorithm — never loses.
	Hard
)

// difficultyNames maps each Difficulty to its display string.
var difficultyNames = map[Difficulty]string{
	Easy: "easy",
	Hard: "hard",
}

// String returns the human-readable name for a difficulty level.
func (d Difficulty) String() string {
	if name, ok := difficultyNames[d]; ok {
		return name
	}
	return "unknown"
}
