package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"tictactoe/ai"
)

func newTestHandler() *Handler {
	return NewHandler(NewService(ai.Hard))
}

func TestHandler_GetState(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var state GameState
	if err := json.NewDecoder(w.Body).Decode(&state); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if state.State != "playing" {
		t.Errorf("expected playing, got %s", state.State)
	}
}

func TestHandler_GetState_ContentType(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}
}

func TestHandler_Play_ValidMove(t *testing.T) {
	h := newTestHandler()
	body, _ := json.Marshal(MoveRequest{Row: 1, Col: 1})
	req := httptest.NewRequest(http.MethodPost, "/api/play", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var state GameState
	json.NewDecoder(w.Body).Decode(&state)
	if state.Board[1][1] != "X" {
		t.Errorf("expected X at (1,1), got %s", state.Board[1][1])
	}
}

func TestHandler_Play_InvalidBody(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/play", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandler_Play_InvalidPosition(t *testing.T) {
	h := newTestHandler()
	body, _ := json.Marshal(MoveRequest{Row: 9, Col: 0})
	req := httptest.NewRequest(http.MethodPost, "/api/play", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", w.Code)
	}

	var errResp ErrorResponse
	json.NewDecoder(w.Body).Decode(&errResp)
	if errResp.Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestHandler_Reset_ValidDifficulty(t *testing.T) {
	h := newTestHandler()

	// Play a move first.
	playBody, _ := json.Marshal(MoveRequest{Row: 0, Col: 0})
	playReq := httptest.NewRequest(http.MethodPost, "/api/play", bytes.NewReader(playBody))
	playW := httptest.NewRecorder()
	h.ServeHTTP(playW, playReq)

	// Reset.
	resetBody, _ := json.Marshal(ResetRequest{Difficulty: "easy"})
	resetReq := httptest.NewRequest(http.MethodPost, "/api/reset", bytes.NewReader(resetBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, resetReq)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var state GameState
	json.NewDecoder(w.Body).Decode(&state)
	if state.Difficulty != "easy" {
		t.Errorf("expected easy, got %s", state.Difficulty)
	}
	if state.State != "playing" {
		t.Errorf("expected playing, got %s", state.State)
	}
}

func TestHandler_Reset_InvalidBody(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/reset", bytes.NewReader([]byte("bad")))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandler_Reset_InvalidDifficulty(t *testing.T) {
	h := newTestHandler()
	body, _ := json.Marshal(ResetRequest{Difficulty: "extreme"})
	req := httptest.NewRequest(http.MethodPost, "/api/reset", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", w.Code)
	}
}

func TestHandler_StaticFiles(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for static index, got %d", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Error("expected non-empty body for index.html")
	}
}
