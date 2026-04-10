//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	tmock "github.com/stretchr/testify/mock"

	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// All messaging handlers check s.service.Messaging() first.
// When MessagingSvc is nil (default), they return 501 Not Implemented.

func TestHandleListMessagingChannels_NotConfigured(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	defer cleanup()

	mock.On("Messaging").Return(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels", nil)
	w := httptest.NewRecorder()

	server.HandleListMessagingChannels(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	assert.Contains(t, w.Body.String(), "not configured")
}

func TestHandleListMessagingChannels_Success(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	defer cleanup()

	svc := &stubMessagingService{}
	mock.On("SetMessagingService", tmock.Anything).Return()
	mock.On("Messaging").Return(svc)
	mock.SetMessagingService(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels", nil)
	w := httptest.NewRecorder()

	server.HandleListMessagingChannels(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "channels")
}

func TestHandleCreateMessagingChannel_NotConfigured(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	defer cleanup()

	mock.On("Messaging").Return(nil)

	body := `{"name":"test-channel","codec":"json","persistent":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/channels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCreateMessagingChannel(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestHandleCreateMessagingChannel_Validation(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/channels", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleCreateMessagingChannel(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("missing channel name", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		body := `{"name":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/channels", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleCreateMessagingChannel(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "channel name is required")
	})

	t.Run("with all options", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		body := `{"name":"test-channel","codec":"json","persistent":true,"max_history":100,"buffer_size":50,"max_subscribers":10,"ttl_seconds":3600}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/channels", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleCreateMessagingChannel(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})
}

func TestHandleGetMessagingChannel_NotConfigured(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	defer cleanup()

	mock.On("Messaging").Return(nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-channel")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels/test-channel", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleGetMessagingChannel(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestHandleGetMessagingChannel_Validation(t *testing.T) {
	t.Run("missing channel name", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleGetMessagingChannel(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "channel name is required")
	})

	t.Run("URL encoded channel name", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test%2Fchannel") // URL encoded /

		req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/channels/test%2Fchannel", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleGetMessagingChannel(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHandleDeleteMessagingChannel_NotConfigured(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	defer cleanup()

	mock.On("Messaging").Return(nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-channel")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/messaging/channels/test-channel", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleDeleteMessagingChannel(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestHandleDeleteMessagingChannel_Validation(t *testing.T) {
	t.Run("missing channel name", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/messaging/channels/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleDeleteMessagingChannel(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "channel name is required")
	})

	t.Run("URL encoded channel name", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test%2Fchannel") // URL encoded /

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/messaging/channels/test%2Fchannel", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleDeleteMessagingChannel(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHandleMessagingSubscribe_NotConfigured(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	defer cleanup()

	mock.On("Messaging").Return(nil)

	body := `{"channel":"test-channel","subscriber_id":"sub-001"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleMessagingSubscribe(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestHandleMessagingSubscribe_Validation(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/subscribe", strings.NewReader("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleMessagingSubscribe(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("missing channel name", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		body := `{"channel":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/subscribe", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleMessagingSubscribe(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "channel name is required")
	})

	t.Run("with all options", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		body := `{"channel":"test-channel","session_id":"sess-001","delta_mode":true,"include_old_values":true,"dedup_metadata":true,"dedup_skip_duplicates":true,"buffer_size":100,"event_types":["PUT","DELETE"],"key_prefix":"prefix/"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/subscribe", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleMessagingSubscribe(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("with lowercase event types", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		body := `{"channel":"test-channel","event_types":["put","delete"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/subscribe", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleMessagingSubscribe(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})
}

func TestHandleMessagingUnsubscribe_NotConfigured(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	defer cleanup()

	mock.On("Messaging").Return(nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "sub-001")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/messaging/subscribe/sub-001", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleMessagingUnsubscribe(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestHandleMessagingUnsubscribe_Validation(t *testing.T) {
	t.Run("missing subscription ID", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/messaging/subscribe/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleMessagingUnsubscribe(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "subscription ID is required")
	})

	t.Run("successful unsubscribe", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "sub-001")

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/messaging/subscribe/sub-001", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleMessagingUnsubscribe(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHandleMessagingPublish_NotConfigured(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	defer cleanup()

	mock.On("Messaging").Return(nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "test-channel")

	body := `{"payload":"test message"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish/test-channel", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleMessagingPublish(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestHandleMessagingPublish_Validation(t *testing.T) {
	t.Run("missing channel name", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("channel", "")

		body := `{"data":"test message"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish/", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleMessagingPublish(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "channel name is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("channel", "test-channel")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish/test-channel", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleMessagingPublish(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("successful publish with all options", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("channel", "test-channel")

		body := `{"payload":"test message","message_id":"msg-001","dedup_key":"key-001","codec":"json","metadata":{"foo":"bar"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish/test-channel", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleMessagingPublish(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("URL encoded channel name", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("channel", "test%2Fchannel")

		body := `{"payload":"test message"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish/test%2Fchannel", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleMessagingPublish(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHandleMessagingPublishDelta_NotConfigured(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	defer cleanup()

	mock.On("Messaging").Return(nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "test-channel")

	body := `{"delta":{"key":"value"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish-delta/test-channel", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleMessagingPublishDelta(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestHandleMessagingPublishDelta_Validation(t *testing.T) {
	t.Run("missing channel name", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("channel", "")

		body := `{"old":{"key":"old"},"new":{"key":"new"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish-delta/", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleMessagingPublishDelta(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "channel name is required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("channel", "test-channel")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish-delta/test-channel", strings.NewReader("{invalid"))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleMessagingPublishDelta(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid JSON body")
	})

	t.Run("missing old value", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("channel", "test-channel")

		body := `{"new":{"key":"new"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish-delta/test-channel", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleMessagingPublishDelta(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "old value is required")
	})

	t.Run("missing new value", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("channel", "test-channel")

		body := `{"old":{"key":"old"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish-delta/test-channel", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleMessagingPublishDelta(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "new value is required")
	})

	t.Run("invalid old value JSON", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("channel", "test-channel")

		// old is raw JSON that is not valid when parsed
		body := `{"old":"invalid","new":{"key":"new"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish-delta/test-channel", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleMessagingPublishDelta(w, req)

		// "invalid" as a string is valid JSON, so this will succeed.
		// To test invalid old JSON, we need to send actual invalid JSON inside the field
		// But the body is valid JSON, so "old": is parsed as json.RawMessage.
		// Actually this should succeed because "invalid" is a valid JSON string
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("successful publish delta with all options", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("channel", "test-channel")

		body := `{"old":{"key":"old"},"new":{"key":"new"},"message_id":"msg-001","dedup_key":"key-001","codec":"json","metadata":{"foo":"bar"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish-delta/test-channel", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleMessagingPublishDelta(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "published")
	})

	t.Run("URL encoded channel name", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("channel", "test%2Fchannel")

		body := `{"old":{"key":"old"},"new":{"key":"new"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/messaging/publish-delta/test%2Fchannel", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleMessagingPublishDelta(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHandleMessagingStats_NotConfigured(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	defer cleanup()

	mock.On("Messaging").Return(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/stats", nil)
	w := httptest.NewRecorder()

	server.HandleMessagingStats(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestHandleMessagingStats_Success(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	defer cleanup()

	svc := &stubMessagingService{}
	mock.On("SetMessagingService", tmock.Anything).Return()
	mock.On("Messaging").Return(svc)
	mock.SetMessagingService(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/stats", nil)
	w := httptest.NewRecorder()

	server.HandleMessagingStats(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "total_channels")
}

func TestHandleMessagingDedupStats_NotConfigured(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	defer cleanup()

	mock.On("Messaging").Return(nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("channel", "test-channel")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/dedup/stats/test-channel", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleMessagingDedupStats(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestHandleMessagingDedupStats_Validation(t *testing.T) {
	t.Run("missing channel name", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("channel", "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/dedup/stats/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleMessagingDedupStats(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "channel name is required")
	})

	t.Run("service type mismatch - not PushService", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		// stubMessagingService is not *messaging.PushService so it will return 501
		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("channel", "test-channel")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/dedup/stats/test-channel", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleMessagingDedupStats(w, req)

		assert.Equal(t, http.StatusNotImplemented, w.Code)
	})

	t.Run("URL encoded channel name", func(t *testing.T) {
		server, mock, cleanup := setupMockServer(t)
		defer cleanup()

		svc := &stubMessagingService{}
		mock.On("SetMessagingService", tmock.Anything).Return()
		mock.On("Messaging").Return(svc)
		mock.SetMessagingService(svc)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("channel", "test%2Fchannel")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/messaging/dedup/stats/test%2Fchannel", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		server.HandleMessagingDedupStats(w, req)

		// Still returns 501 because not PushService, but channel name was decoded
		assert.Equal(t, http.StatusNotImplemented, w.Code)
	})
}

func TestMessagingServiceNotConfiguredError(t *testing.T) {
	err := &MessagingServiceNotConfiguredError{}
	assert.Equal(t, "messaging notification service is not configured", err.Error())
}

// stubMessagingService is a minimal stub for the messaging.Service interface.
type stubMessagingService struct{}

func (s *stubMessagingService) CreateChannel(ctx context.Context, name string, opts ...messaging.CreateChannelOption) (*messaging.Channel, error) {
	return &messaging.Channel{
		Name:       name,
		Codec:      "json",
		Persistent: false,
	}, nil
}

func (s *stubMessagingService) GetChannel(ctx context.Context, name string) (*messaging.Channel, error) {
	return &messaging.Channel{
		Name:       name,
		Codec:      "json",
		Persistent: false,
	}, nil
}

func (s *stubMessagingService) DeleteChannel(ctx context.Context, name string) error {
	return nil
}

func (s *stubMessagingService) ListChannels(ctx context.Context) ([]*messaging.Channel, error) {
	return []*messaging.Channel{}, nil
}

func (s *stubMessagingService) Subscribe(ctx context.Context, channel string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	return &messaging.Subscription{
		ID:        "sub-001",
		Channel:   channel,
		ClientID:  "client-001",
		CreatedAt: time.Now(),
	}, nil
}

func (s *stubMessagingService) Unsubscribe(ctx context.Context, subscriptionID string) error {
	return nil
}

func (s *stubMessagingService) Publish(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
	return &messaging.PublishResult{MessageID: "mock-id"}, nil
}

func (s *stubMessagingService) PublishDelta(ctx context.Context, channel string, old, new interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
	return &messaging.PublishResult{MessageID: "mock-id"}, nil
}

func (s *stubMessagingService) WatchChannel(ctx context.Context, keyPrefix string, opts ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	return &messaging.Subscription{ID: "watch-001"}, nil
}

func (s *stubMessagingService) Start(ctx context.Context) error {
	return nil
}

func (s *stubMessagingService) Stop(ctx context.Context) error {
	return nil
}

func (s *stubMessagingService) IsRunning() bool {
	return true
}

func (s *stubMessagingService) GetStats() *messaging.Stats {
	return &messaging.Stats{
		TotalChannels:      5,
		TotalSubscriptions: 10,
		MessagesPublished:  1000,
		MessagesDelivered:  950,
		DedupHits:          50,
		DroppedMessages:    2,
		BufferUtilization:  0.75,
	}
}
