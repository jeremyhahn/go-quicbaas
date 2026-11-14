// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"net/url"
	"strings"

	servicesclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/services"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// The service plane over REST (and QUIC, this REST dialect over HTTP/3). The
// routes are quicbaasd's /api/v1/services mount
// (pkg/api/rest/handlers_services.go).

// isServicesService reports whether the service name targets the service
// plane.
func isServicesService(service string) bool {
	return strings.HasPrefix(service, servicesclient.ServiceName)
}

// servicesNamedRoutes maps the verbs that address a service by name to their
// HTTP method and the path suffix after /api/v1/services/{name}.
var servicesNamedRoutes = map[string]struct{ method, suffix string }{
	"Get":      {http.MethodGet, ""},
	"Undeploy": {http.MethodDelete, ""},
	"Start":    {http.MethodPost, "/start"},
	"Stop":     {http.MethodPost, "/stop"},
	"Resume":   {http.MethodPost, "/resume"},
	"Restart":  {http.MethodPost, "/restart"},
}

// handleServicesRequest dispatches a service plane verb.
func (t *Transport) handleServicesRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	op := "rest.services." + method
	switch method {
	case "Deploy", "Redeploy":
		r, ok := req.(*servicesclient.DeployRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *services.DeployRequest")
		}
		verb := http.MethodPost
		if method == "Redeploy" {
			verb = http.MethodPut
		}
		var rec servicesclient.Record
		if err := t.doJSONRequest(ctx, verb, "/api/v1/services", r, &rec); err != nil {
			return nil, err
		}
		return &rec, nil
	case "List":
		var resp servicesclient.ListResponse
		if err := t.doJSONRequest(ctx, http.MethodGet, "/api/v1/services", nil, &resp); err != nil {
			return nil, err
		}
		return &resp, nil
	}
	route, ok := servicesNamedRoutes[method]
	if !ok {
		return nil, errors.NewUnimplemented(op, "method not exposed by the services REST surface")
	}
	r, ok := req.(*servicesclient.NameRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type, expected *services.NameRequest")
	}
	path := "/api/v1/services/" + url.PathEscape(r.Name) + route.suffix
	if method == "Undeploy" {
		return nil, t.doJSONRequest(ctx, route.method, path, nil, nil)
	}
	var rec servicesclient.Record
	if err := t.doJSONRequest(ctx, route.method, path, nil, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}
