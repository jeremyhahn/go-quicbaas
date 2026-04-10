//go:build rest

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestHandleMessagingChannelStats_NotConfigured tests channel stats returns 501
// when messaging service is not configured.
func TestHandleMessagingChannelStats_NotConfigured(t *testing.T) {
	server, m, cleanup := setupMockServer(t)
	defer cleanup()

	m.On("Messaging").Return(nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "test-channel")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels/test-channel/stats", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleMessagingChannelStats(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	assert.Contains(t, w.Body.String(), "not configured")
}

// TestHandleMessagingChannelStats_MissingChannel tests channel stats returns 400
// when the channel name URL parameter is empty.
func TestHandleMessagingChannelStats_MissingChannel(t *testing.T) {
	server, m, cleanup := setupMockServer(t)
	defer cleanup()

	m.On("SetMessagingService", mock.Anything).Return()
	m.On("Messaging").Return(&stubMessagingService{})
	m.SetMessagingService(&stubMessagingService{})

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels//stats", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleMessagingChannelStats(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "channel name is required")
}

// TestHandleMessagingChannelStats_Success tests successful channel stats retrieval.
func TestHandleMessagingChannelStats_Success(t *testing.T) {
	server, m, cleanup := setupMockServer(t)
	defer cleanup()

	m.On("SetMessagingService", mock.Anything).Return()
	m.On("Messaging").Return(&stubMessagingService{})
	m.SetMessagingService(&stubMessagingService{})

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "test-channel")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels/test-channel/stats", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleMessagingChannelStats(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "test-channel")
}

// TestHandleMessagingChannelStats_URLEncoded tests channel stats with URL-encoded channel name.
func TestHandleMessagingChannelStats_URLEncoded(t *testing.T) {
	server, m, cleanup := setupMockServer(t)
	defer cleanup()

	m.On("SetMessagingService", mock.Anything).Return()
	m.On("Messaging").Return(&stubMessagingService{})
	m.SetMessagingService(&stubMessagingService{})

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "test%2Fchannel")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels/test%2Fchannel/stats", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleMessagingChannelStats(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "test/channel")
}

// TestHandleListMessagingChannels_WithNamePrefix tests channel listing
// with the name_prefix query parameter filter.
func TestHandleListMessagingChannels_WithNamePrefix(t *testing.T) {
	server, m, cleanup := setupMockServer(t)
	defer cleanup()

	m.On("SetMessagingService", mock.Anything).Return()
	m.On("Messaging").Return(&stubMessagingService{})
	m.SetMessagingService(&stubMessagingService{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels?name_prefix=test", nil)
	w := httptest.NewRecorder()

	server.HandleListMessagingChannels(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "channels")
}
