package web

import (
	"math/rand/v2"
	"sync"
	"tictactoe/ai"
	"tictactoe/game"
)

// difficultyMap provides O(1) lookup from string to ai.Difficulty.
var difficultyMap = map[string]ai.Difficulty{
	"easy": ai.Easy,
	"hard": ai.Hard,
}

// GameState is the JSON-serializable representation of the current game.
type GameState struct {
	Board      [3][3]string `json:"board"`
	Current    string       `json:"current"`
	State      string       `json:"state"`
	Difficulty string       `json:"difficulty"`
	Winner     string       `json:"winner"`
}

// Service manages game state and coordinates between the game engine and AI.
type Service struct {
	mu         sync.Mutex
	game       *game.Game
	aiPlayer   *ai.Player
	difficulty ai.Difficulty
}

// NewService creates a new game service with the specified difficulty.
func NewService(difficulty ai.Difficulty) *Service {
	rng := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	return &Service{
		game:       game.NewGame(),
		aiPlayer:   ai.NewPlayer(game.O, difficulty, rng),
		difficulty: difficulty,
	}
}

// State returns the current game state as a serializable struct.
func (s *Service) State() GameState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buildState()
}

// Play executes a human move at (row, col), then lets the AI respond if the
// game is still in progress. Returns the updated game state.
func (s *Service) Play(row, col int) (GameState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.game.Play(row, col); err != nil {
		return s.buildState(), err
	}

	// AI responds if game is still playing.
	if s.game.State() == game.Playing {
		move, err := s.aiPlayer.ChooseMove(s.game)
		if err != nil {
			return s.buildState(), err
		}
		if err := s.game.Play(move.Row, move.Col); err != nil {
			return s.buildState(), err
		}
	}

	return s.buildState(), nil
}

// Reset starts a new game with the given difficulty.
func (s *Service) Reset(difficultyStr string) (GameState, error) {
	d, ok := difficultyMap[difficultyStr]
	if !ok {
		return GameState{}, &InvalidDifficultyError{Value: difficultyStr}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rng := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	s.game = game.NewGame()
	s.aiPlayer = ai.NewPlayer(game.O, d, rng)
	s.difficulty = d

	return s.buildState(), nil
}

// buildState converts the internal game into a JSON-friendly struct.
// Caller must hold s.mu.
func (s *Service) buildState() GameState {
	var board [3][3]string
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			cell, _ := s.game.Board().Cell(r, c)
			board[r][c] = cell.String()
		}
	}

	winner := ""
	switch s.game.State() {
	case game.XWins:
		winner = "X"
	case game.OWins:
		winner = "O"
	}

	return GameState{
		Board:      board,
		Current:    s.game.CurrentPlayer().String(),
		State:      s.game.State().String(),
		Difficulty: s.difficulty.String(),
		Winner:     winner,
	}
}
