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

//go:build timeseries

package server

// initTimeSeriesStorage is a no-op. The qrdb node always owns time-series
// storage; startTimeSeries wires the node's SDK transport directly.
func (s *Server) initTimeSeriesStorage() error {
	// The qrdb engine node owns the time series storage and at-rest encryption.
	// Building a separate PebbleDB store here would create a disconnected
	// duplicate. startTimeSeries wires the node-backed adapter.
	s.logger.Info("Time series storage owned by qrdb node, skipping standalone storage init")
	return nil
}

// startTimeSeries wires the time-series storage adapter that delegates through
// the qrdb node's SDK client embedded transport to the node's in-process time
// series engine. The node's lifecycle owns the underlying store.
func (s *Server) startTimeSeries() error {
	if !s.config.TimeSeries.Enabled {
		s.logger.Info("Time series service is disabled, skipping initialization")
		return nil
	}

	s.timeSeriesStorage = newNodeTimeSeriesStorage(s.getQRDBNode().Client())
	s.logger.Info("Time series storage wired via qrdb node SDK transport")
	return nil
}

// stopTimeSeries closes the time series storage and releases all resources.
// On the node path timeSeriesStorage is a nodeTimeSeriesStorage whose Close
// is a no-op — the qrdb node owns the underlying store lifecycle.
func (s *Server) stopTimeSeries() error {
	if s.timeSeriesStorage == nil {
		return nil
	}

	s.logger.Info("Shutting down time series storage...")

	s.timeSeriesStorage = nil
	s.logger.Info("Time series storage shutdown complete")

	return nil
}
