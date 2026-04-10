package web

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFiles embed.FS

// MoveRequest is the JSON body for the play endpoint.
type MoveRequest struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

// ResetRequest is the JSON body for the reset endpoint.
type ResetRequest struct {
	Difficulty string `json:"difficulty"`
}

// ErrorResponse is the JSON body for error responses.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Handler builds the HTTP handler tree for the game API and static files.
type Handler struct {
	service *Service
	mux     *http.ServeMux
}

// NewHandler creates a new Handler wired to the given service.
func NewHandler(service *Service) *Handler {
	h := &Handler{
		service: service,
		mux:     http.NewServeMux(),
	}
	h.registerRoutes()
	return h
}

// ServeHTTP delegates to the internal mux.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// registerRoutes wires API endpoints and static file serving.
func (h *Handler) registerRoutes() {
	h.mux.HandleFunc("GET /api/state", h.handleState)
	h.mux.HandleFunc("POST /api/play", h.handlePlay)
	h.mux.HandleFunc("POST /api/reset", h.handleReset)

	// Serve embedded static files at root.
	staticFS, _ := fs.Sub(staticFiles, "static")
	h.mux.Handle("/", http.FileServer(http.FS(staticFS)))
}

// handleState returns the current game state.
func (h *Handler) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.service.State())
}

// handlePlay processes a human move and returns the updated state.
func (h *Handler) handlePlay(w http.ResponseWriter, r *http.Request) {
	var req MoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	state, err := h.service.Play(req.Row, req.Col)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, state)
}

// handleReset starts a new game with the requested difficulty.
func (h *Handler) handleReset(w http.ResponseWriter, r *http.Request) {
	var req ResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	state, err := h.service.Reset(req.Difficulty)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, state)
}

// writeJSON encodes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
