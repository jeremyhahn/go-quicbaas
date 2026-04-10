package game

// Score tracks wins, losses, and draws across multiple games.
type Score struct {
	XWins  int
	OWins  int
	Draws  int
	Played int
}

// Record updates the score based on a game result state.
// Only records terminal states (XWins, OWins, Draw); Playing is ignored.
func (s *Score) Record(state State) {
	switch state {
	case XWins:
		s.XWins++
		s.Played++
	case OWins:
		s.OWins++
		s.Played++
	case Draw:
		s.Draws++
		s.Played++
	}
}

// Reset clears all score counters.
func (s *Score) Reset() {
	s.XWins = 0
	s.OWins = 0
	s.Draws = 0
	s.Played = 0
}
