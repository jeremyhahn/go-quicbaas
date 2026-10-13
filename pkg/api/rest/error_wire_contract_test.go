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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// errorDocPath is the API reference whose error-code table clients build
// against; TestErrorDocTableMatchesErrorCodes keeps it honest.
var errorDocPath = filepath.Join("..", "..", "..", "docs", "multiprotocol-api", "api.md")

// errorBodyKeys decodes a REST error response and returns its top-level
// field names, sorted, together with the raw fields.
func errorBodyKeys(t *testing.T, w *httptest.ResponseRecorder) ([]string, map[string]json.RawMessage) {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v (body %s)", err, w.Body.String())
	}
	keys := make([]string, 0, len(body))
	for k := range body {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, body
}

// TestErrorWireContract pins the REST error body a client receives for each
// error family: a flat object, never a nested "error" envelope, with
// lower-case codes. The client-error classes that go through the core
// service-error mapping currently carry no "code" field; the test pins that
// too, so a change to it is a deliberate contract change and not drift.
func TestErrorWireContract(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	serviceError := func(code qberrors.ErrorCode) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			server.handleServiceError(w, r, qberrors.New(code, "entity.get", "user/1"))
		}
	}

	tests := []struct {
		name      string
		respond   func(http.ResponseWriter, *http.Request)
		status    int
		code      string // empty: the body has no code field
		retryable bool
	}{
		{"not_found", serviceError(qberrors.ErrNotFound), http.StatusNotFound, "", false},
		{"invalid_input", serviceError(qberrors.ErrInvalidInput), http.StatusBadRequest, "", false},
		{"unauthorized", serviceError(qberrors.ErrUnauthorized), http.StatusUnauthorized, "", false},
		{"forbidden", serviceError(qberrors.ErrForbidden), http.StatusForbidden, "", false},
		{"conflict", serviceError(qberrors.ErrConflict), http.StatusConflict, "", false},
		{"timeout", serviceError(qberrors.ErrTimeout), http.StatusRequestTimeout, "", false},
		{"internal", serviceError(qberrors.ErrInternal), http.StatusInternalServerError, "internal", false},
		{"unavailable", serviceError(qberrors.ErrUnavailable), http.StatusServiceUnavailable, "unavailable", true},
		{"leader_not_available", serviceError(qberrors.ErrLeaderNotAvailable), http.StatusServiceUnavailable, "leader_not_available", true},
		{"rate_limited", func(w http.ResponseWriter, r *http.Request) {
			RespondError(w, r, http.StatusTooManyRequests, &RateLimitError{Limit: 1000, Period: "1s"})
		}, http.StatusTooManyRequests, "rate_limited", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tt.respond(w, httptest.NewRequest(http.MethodGet, "/api/v1/entities/user/1", nil))

			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
			keys, body := errorBodyKeys(t, w)
			wantKeys := []string{"error", "retryable", "timestamp"}
			if tt.code != "" {
				wantKeys = []string{"code", "error", "retryable", "timestamp"}
			}
			if strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
				t.Fatalf("fields = %v, want %v (body %s)", keys, wantKeys, w.Body.String())
			}

			var message string
			if err := json.Unmarshal(body["error"], &message); err != nil || message == "" {
				t.Errorf("error = %s, want a non-empty string: the body is flat, not an envelope", body["error"])
			}
			if tt.code != "" {
				var code string
				if err := json.Unmarshal(body["code"], &code); err != nil || code != tt.code {
					t.Errorf("code = %s, want %q", body["code"], tt.code)
				}
			}
			var retryable bool
			if err := json.Unmarshal(body["retryable"], &retryable); err != nil || retryable != tt.retryable {
				t.Errorf("retryable = %s, want %v", body["retryable"], tt.retryable)
			}
		})
	}
}

// documentedErrorCodes maps every code string in pkg/errors to its code, so
// the doc test can tell a real code from an invented one.
func documentedErrorCodes() map[string]qberrors.ErrorCode {
	all := []qberrors.ErrorCode{
		qberrors.ErrUnknown, qberrors.ErrNotFound, qberrors.ErrConflict, qberrors.ErrTimeout,
		qberrors.ErrInternal, qberrors.ErrInvalidInput, qberrors.ErrUnauthorized, qberrors.ErrForbidden,
		qberrors.ErrUnavailable, qberrors.ErrStorage, qberrors.ErrRaft, qberrors.ErrEncryption,
		qberrors.ErrDecryption, qberrors.ErrValidation, qberrors.ErrConfiguration, qberrors.ErrCanceled,
		qberrors.ErrDeadlineExceeded, qberrors.ErrAlreadyExists, qberrors.ErrResourceExhausted,
		qberrors.ErrPreconditionFailed, qberrors.ErrAborted, qberrors.ErrOutOfRange, qberrors.ErrUnimplemented,
		qberrors.ErrDataLoss, qberrors.ErrSnapshot, qberrors.ErrReplication, qberrors.ErrLeaderNotAvailable,
		qberrors.ErrClusterNotReady, qberrors.ErrQuorumLost, qberrors.ErrSplitBrain, qberrors.ErrRateLimited,
		qberrors.ErrNotImplemented, qberrors.ErrNotAllowed, qberrors.ErrSystemKeyspaceViolation,
		qberrors.ErrCrossTenantAccess, qberrors.ErrMissingTenantContext, qberrors.ErrMissingJustification,
		qberrors.ErrMissingUserContext, qberrors.ErrCircuitOpen,
	}
	codes := make(map[string]qberrors.ErrorCode, len(all))
	for _, c := range all {
		codes[c.String()] = c
	}
	return codes
}

// errorTableHeader opens the error-code table in the doc.
const errorTableHeader = "| Code | HTTP Status | Retryable |"

// errorTableRows returns the cells of the error-code table under the doc's
// "## Error Handling" heading, one slice of trimmed cells per data row.
func errorTableRows(t *testing.T, doc string) [][]string {
	t.Helper()
	_, section, found := strings.Cut(doc, "\n## Error Handling\n")
	if !found {
		t.Fatalf("%s has no \"## Error Handling\" section", errorDocPath)
	}
	_, table, found := strings.Cut(section, "\n"+errorTableHeader)
	if !found {
		t.Fatalf("%s: no table headed %q under \"## Error Handling\"", errorDocPath, errorTableHeader)
	}
	var rows [][]string
	for _, line := range strings.Split(table, "\n") {
		if !strings.HasPrefix(line, "|") {
			if len(rows) > 0 {
				break
			}
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if strings.HasPrefix(cells[0], "`") {
			rows = append(rows, cells)
		}
	}
	return rows
}

// TestErrorDocTableMatchesErrorCodes reads the API reference's error-code
// table and checks every row against pkg/errors: the code exists, and its
// HTTP status and retryability are the ones the code itself declares.
func TestErrorDocTableMatchesErrorCodes(t *testing.T) {
	doc, err := os.ReadFile(errorDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", errorDocPath, err)
	}
	rows := errorTableRows(t, string(doc))
	if len(rows) == 0 {
		t.Fatalf("%s: no error-code rows found under \"## Error Handling\"", errorDocPath)
	}

	codes := documentedErrorCodes()
	for _, row := range rows {
		if len(row) < 3 {
			t.Errorf("row %v: want code, HTTP status and retryable columns", row)
			continue
		}
		name := strings.Trim(row[0], "`")
		code, ok := codes[name]
		if !ok {
			t.Errorf("documented code %q does not exist in pkg/errors", name)
			continue
		}
		if want := strconv.Itoa(code.HTTPStatus()); row[1] != want {
			t.Errorf("%s: documented HTTP status %s, pkg/errors says %s", name, row[1], want)
		}
		want := "no"
		if code.IsRetryable() {
			want = "yes"
		}
		if row[2] != want {
			t.Errorf("%s: documented retryable %q, pkg/errors says %q", name, row[2], want)
		}
	}
}
