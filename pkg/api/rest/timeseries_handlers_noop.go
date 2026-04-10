//go:build rest && !timeseries

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
	"errors"
	"net/http"
)

// ErrTimeseriesNotEnabled is returned when timeseries feature is not compiled.
var ErrTimeseriesNotEnabled = errors.New("timeseries support not compiled - build with -tags timeseries")

// HandleWritePoint is a stub when timeseries is not enabled.
func (s *Server) HandleWritePoint(w http.ResponseWriter, r *http.Request) {
	RespondError(w, r, http.StatusNotImplemented, ErrTimeseriesNotEnabled)
}

// HandleWriteBatch is a stub when timeseries is not enabled.
func (s *Server) HandleWriteBatch(w http.ResponseWriter, r *http.Request) {
	RespondError(w, r, http.StatusNotImplemented, ErrTimeseriesNotEnabled)
}

// HandleTSQuery is a stub when timeseries is not enabled.
func (s *Server) HandleTSQuery(w http.ResponseWriter, r *http.Request) {
	RespondError(w, r, http.StatusNotImplemented, ErrTimeseriesNotEnabled)
}

// HandleDeleteSeries is a stub when timeseries is not enabled.
func (s *Server) HandleDeleteSeries(w http.ResponseWriter, r *http.Request) {
	RespondError(w, r, http.StatusNotImplemented, ErrTimeseriesNotEnabled)
}

// HandleGetRetentionPolicy is a stub when timeseries is not enabled.
func (s *Server) HandleGetRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	RespondError(w, r, http.StatusNotImplemented, ErrTimeseriesNotEnabled)
}

// HandleSetRetentionPolicy is a stub when timeseries is not enabled.
func (s *Server) HandleSetRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	RespondError(w, r, http.StatusNotImplemented, ErrTimeseriesNotEnabled)
}

// HandleListMetrics is a stub when timeseries is not enabled.
func (s *Server) HandleListMetrics(w http.ResponseWriter, r *http.Request) {
	RespondError(w, r, http.StatusNotImplemented, ErrTimeseriesNotEnabled)
}
