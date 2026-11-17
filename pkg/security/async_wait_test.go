package security

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// asyncTimeout bounds every wait on background work in this package's tests.
// It is a ceiling, not a delay: each wait returns as soon as the work is done,
// so a slow machine makes a test take longer to pass instead of failing it.
const asyncTimeout = 10 * time.Second

// asyncPoll is how often require.Eventually re-checks a background condition.
const asyncPoll = 5 * time.Millisecond

// siemRequest is one request a SIEM capture server received.
type siemRequest struct {
	header http.Header
	body   string
}

// newSIEMCaptureServer starts an HTTP server that answers 200 to every request
// and hands each request to the returned channel, so a test waits on delivery
// itself rather than on a guess at how long delivery takes.
func newSIEMCaptureServer(t *testing.T) (*httptest.Server, <-chan siemRequest) {
	t.Helper()
	requests := make(chan siemRequest, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case requests <- siemRequest{header: r.Header.Clone(), body: string(body)}:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server, requests
}

// receiveRequest returns the next request a capture server received, failing
// the test if none arrives within asyncTimeout.
func receiveRequest(t *testing.T, requests <-chan siemRequest) siemRequest {
	t.Helper()
	select {
	case req := <-requests:
		return req
	case <-time.After(asyncTimeout):
		t.Fatalf("no SIEM request arrived within %s", asyncTimeout)
		return siemRequest{}
	}
}
