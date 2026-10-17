//go:build rest

package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func TestHandleServiceError_WrappedInvalidRedirectURIIsBadRequest(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	err := &iam.IAMContextError{
		Message: "failed to update app",
		Cause: &iam.InvalidRedirectURIError{
			URI:    "ftp://app.example/cb",
			Reason: "URI scheme must be HTTPS (or HTTP for localhost)",
		},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/v1/iam/apps/c1", nil)
	server.handleServiceError(w, r, err)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, errorBody(t, w), "URI scheme must be HTTPS")
}

// An IAMContextError that does not carry a redirect URI refusal also wraps
// storage failures, so it must stay a server error.
func TestHandleServiceError_ContextErrorWithoutRedirectURIStaysInternal(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	err := &iam.IAMContextError{Message: "failed to store app", Cause: unrelatedServiceError{}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/v1/iam/apps/c1", nil)
	server.handleServiceError(w, r, err)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
