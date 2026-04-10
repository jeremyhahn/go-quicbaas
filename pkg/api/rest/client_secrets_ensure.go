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
	"fmt"
	"net/http"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// EnsureSecret invokes the Vault-style generate-on-first-access REST
// endpoint. On the first call for a given path the server generates a
// random value per the requested spec and stores it; subsequent calls
// return the same stable bytes. Spec mismatch against a previously
// stored value is a hard error.
func (c *RESTClient) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("RESTClient.EnsureSecret", "request")
	}
	if req.SecretID == "" {
		return nil, api.NewInvalidInputError("RESTClient.EnsureSecret: secret_id is required")
	}

	endpoint := fmt.Sprintf("/api/v1/secrets/%s/ensure", req.SecretID)

	var resp service.EnsureSecretResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
