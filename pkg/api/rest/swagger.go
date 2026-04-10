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
	"net/http"
	"os"
	"path/filepath"
)

// HandleOpenAPISpec serves the OpenAPI specification.
// CORS headers are handled by the central CORSMiddleware rather than
// being set per-handler, ensuring consistent cross-origin policy.
func (s *Server) HandleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	// Try to load the OpenAPI spec from the expected location
	// This works both in development and when properly deployed
	specPath := filepath.Join("api", "openapi", "quicbaas-v1.yaml")

	spec, err := os.ReadFile(specPath)
	if err != nil {
		// Fallback to a minimal spec if file not found
		spec = []byte(`openapi: 3.0.3
info:
  title: QuicBaaS REST API
  description: High-performance distributed key-value database with Raft consensus
  version: 1.0.0
servers:
  - url: /v1
paths:
  /health:
    get:
      summary: Health check
      responses:
        '200':
          description: Server is healthy
`)
		s.logger.Warnf("Could not load OpenAPI spec from %s: %v (using fallback)", specPath, err)
	}

	w.Header().Set("Content-Type", "application/x-yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(spec) // Best effort write
}

// HandleSwaggerUI serves the Swagger UI HTML page.
func (s *Server) HandleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>QuicBaaS API Documentation</title>
    <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
    <style>
        body {
            margin: 0;
            padding: 0;
        }
        .topbar {
            display: none;
        }
        .swagger-ui .info {
            margin: 20px 0;
        }
    </style>
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
    <script>
        window.onload = function() {
            const ui = SwaggerUIBundle({
                url: '/docs/openapi.yaml',
                dom_id: '#swagger-ui',
                deepLinking: true,
                presets: [
                    SwaggerUIBundle.presets.apis,
                    SwaggerUIStandalonePreset
                ],
                plugins: [
                    SwaggerUIBundle.plugins.DownloadUrl
                ],
                layout: "StandaloneLayout",
                defaultModelsExpandDepth: 1,
                defaultModelExpandDepth: 1,
                docExpansion: "list",
                filter: true,
                tryItOutEnabled: true
            });
            window.ui = ui;
        }
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html)) // Best effort write
}

// HandleDocsRedirect redirects /docs to /docs/ for proper URL handling.
func (s *Server) HandleDocsRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/docs/", http.StatusMovedPermanently)
}
