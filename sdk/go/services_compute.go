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

package quicbaas

import (
	"net/http"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/images"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// ImagesClient is the OCI image registry client (sdk/go/client/images).
type ImagesClient = images.Client

// ComputeService is the compute surface of a QuicBaaS node. Today it is the
// OCI image registry served at /v2/ on the node's REST listener; it grows
// with the service plane.
type ComputeService interface {
	// Images returns the client for the node's OCI registry. The client is
	// built once per SDK client from the client's address (origin only) and
	// TLS config, or from WithImagesURL, so it presents the caller's TLS
	// client certificate and keeps any registry session across calls. A
	// client without a TCP address (Unix socket, embedded, or an injected
	// transport) needs WithImagesURL; without it Images returns an
	// InvalidInput error.
	Images() (*ImagesClient, error)
}

type computeService struct{ c *client }

// Compute returns the compute service: the OCI image registry.
func (c *client) Compute() ComputeService { return &computeService{c: c} }

// Images returns the SDK client's OCI registry client.
func (s *computeService) Images() (*ImagesClient, error) { return s.c.images() }

// images builds the OCI registry client once and returns the stored pair.
func (c *client) images() (*ImagesClient, error) {
	c.imagesOnce.Do(func() {
		base, err := imagesBaseURL(c.address, c.opts)
		if err != nil {
			c.imagesErr = err
			return
		}
		tlsCfg := resolveTLSConfig(c.opts)
		if tlsCfg != nil {
			tlsCfg = tlsCfg.Clone()
		}
		// No http.Client.Timeout: blob pulls are bounded by the caller's
		// context.
		hc := &http.Client{Transport: &http.Transport{
			Proxy:             http.ProxyFromEnvironment,
			TLSClientConfig:   tlsCfg,
			ForceAttemptHTTP2: true,
		}}
		ic, err := images.NewClient(base, images.WithHTTPClient(hc))
		if err != nil {
			c.imagesErr = err
			return
		}
		c.imagesClient = ic
	})
	return c.imagesClient, c.imagesErr
}

// imagesBaseURL returns the base URL of the REST listener that serves the OCI
// registry: opts.imagesURL when set, else the origin of address (the REST
// server mounts the registry at its root /v2, so the address's path is not
// used).
func imagesBaseURL(address string, opts *clientOptions) (string, error) {
	if opts == nil {
		opts = defaultOptions()
	}
	if opts.imagesURL != "" {
		return strings.TrimSuffix(opts.imagesURL, "/"), nil
	}
	if address == "" || opts.transportType == TransportUnix || opts.transportType == TransportEmbedded {
		return "", sdkerrors.NewInvalidInput("Compute.Images", "no REST listener address; set WithImagesURL")
	}
	return transport.HTTPBaseURL(address, resolveTLSConfig(opts) != nil), nil
}
