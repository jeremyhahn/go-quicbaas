// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package vector

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// These exercise the transport-error propagation branch of Upsert and
// SearchSimilar, which the primary client_test.go leaves uncovered.

func TestClient_Upsert_TransportError(t *testing.T) {
	req := &UpsertRequest{Vector: &VectorEntity{
		Namespace: "docs", ID: "a", Vector: []float32{0.1},
	}}
	_, err := NewClient(newErrTransport(transportErr)).Upsert(context.Background(), req)
	assertErrCode(t, err, errors.CodeUnavailable)
}

func TestClient_SearchSimilar_TransportError(t *testing.T) {
	req := &SearchSimilarRequest{Namespace: "docs", QueryVector: []float32{0.1}, K: 5}
	_, err := NewClient(newErrTransport(transportErr)).SearchSimilar(context.Background(), req)
	assertErrCode(t, err, errors.CodeUnavailable)
}
