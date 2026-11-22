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

package capabilities

import (
	"context"
	"encoding/json"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	// ServiceName is the transport service name of the capabilities report.
	ServiceName = "quicbaas.Capabilities"
	// MethodGet is the transport method that reads the report.
	MethodGet = "Get"
)

// Subsystem is one subsystem's entry in the report.
type Subsystem struct {
	// Served is true when the server has the subsystem's handlers.
	Served bool `json:"served"`
	// Configured is true when the service those handlers use is wired.
	Configured bool `json:"configured"`
	// Reason is empty when the subsystem is served and configured, and
	// otherwise names the setting or build tag that enables it.
	Reason string `json:"reason"`
}

// Report is the server's capabilities report.
type Report struct {
	// Subsystems maps a subsystem name (timeseries, messaging,
	// session_rotation, xkms_ca, vector, scheduler) to its entry.
	Subsystems map[string]Subsystem `json:"subsystems"`
	// Families lists the SDK families the server serves.
	Families []string `json:"families"`
}

// Available reports whether the named subsystem is served and configured.
func (r *Report) Available(name string) bool {
	if r == nil {
		return false
	}
	sub, ok := r.Subsystems[name]
	return ok && sub.Served && sub.Configured
}

// Client reads the server's capabilities report.
type Client interface {
	// Get returns the server's capabilities report.
	Get(ctx context.Context) (*Report, error)
}

// GetRequest is the (empty) request Get sends.
type GetRequest struct{}

type client struct {
	transport transport.Transport
}

// NewClient returns a capabilities client over t, or nil when t is nil.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{transport: t}
}

// Get returns the server's capabilities report.
func (c *client) Get(ctx context.Context) (*Report, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("capabilities.Get", "context is required")
	}

	resp, err := c.transport.Request(ctx, ServiceName, MethodGet, &GetRequest{})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("capabilities.Get", "nil response from server")
	}
	return decodeReport(resp)
}

// decodeReport accepts the report as a transport returns it: typed by the
// in-process (Embedded) transport, or decoded JSON (a map) by REST and QUIC.
// Any other value is re-encoded as JSON and decoded into a Report.
func decodeReport(resp interface{}) (*Report, error) {
	switch v := resp.(type) {
	case *Report:
		return v, nil
	case Report:
		return &v, nil
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "capabilities.Get", "failed to encode response")
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "capabilities.Get", "invalid response type")
	}
	if report.Subsystems == nil {
		return nil, errors.NewInternal("capabilities.Get", "response carries no subsystems")
	}
	return &report, nil
}
