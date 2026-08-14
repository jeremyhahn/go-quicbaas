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

package server

import (
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/initmarker"
)

// initDoneMarkerName is the filename of the marker dropped into the data dir
// by `quicbaasd init`. Both the writer (cmd/quicbaasd) and this reader import
// the single canonical definition from pkg/initmarker so the two ends can
// never drift.
const initDoneMarkerName = initmarker.FileName

// readInitMarker loads the persisted init-done marker from dataDir via the
// shared pkg/initmarker parser. It returns (nil, nil) when no marker is
// present (a fresh, never-initialised data dir), so callers can fall back to
// the config default. A malformed marker surfaces a DataPlaneBarrierError so
// the operator sees a precise failure rather than a silent fall-through to
// the wrong strategy.
func readInitMarker(dataDir string) (*initmarker.Marker, error) {
	marker, err := initmarker.Read(dataDir)
	if err != nil {
		stage := "read init marker"
		var readErr *initmarker.ReadError
		if errors.As(err, &readErr) && readErr.Stage == "parse" {
			stage = "parse init marker"
		}
		return nil, &DataPlaneBarrierError{Stage: stage, Cause: err}
	}
	return marker, nil
}

// resolveBarrierStrategy returns the barrier strategy parameters the data-plane
// barrier must use, honoring the precedence:
//
//  1. Explicit operator override (operatorOverride == true) — the config /env
//     value in cfg.Seal wins unconditionally. This is the escape hatch for an
//     operator who deliberately runs `quicbaasd start` against a data dir whose
//     init decision they want to supersede.
//  2. Persisted init marker — when no explicit override is present and the data
//     dir holds a `.quicbaasd_init_done` marker, the strategy (and shamir
//     threshold/total) recorded at init time wins. This is the common path that
//     makes `quicbaasd init --barrier-strategy none` followed by
//     `quicbaasd start` come up with the none strategy rather than the
//     "passphrase" config default.
//  3. Config default — a fresh data dir with no marker and no override uses
//     cfg.Seal as-is (DefaultSealConfig => "passphrase").
//
// The returned values are (strategyID, threshold, totalShares, fromMarker).
// threshold/total are only meaningful for the shamir strategy; for
// none/passphrase they are returned unchanged from cfg so non-shamir callers
// can ignore them.
//
// fromMarker reports whether the strategy came from case (2) — a persisted
// init decision — as opposed to an override or the config default. Callers
// that need to distinguish "the operator ran init and chose this" from "nobody
// said anything, so this is the default" must consult it: the config default
// is itself "passphrase", so a non-empty strategy alone proves nothing.
func resolveBarrierStrategy(
	cfg *config.Config,
	operatorOverride bool,
) (qrdbsdk.StrategyID, int, int, bool, error) {
	// Resolve the configured strategy, applying the P4 hardware-prefer
	// auto-detect policy for the "auto" sentinel (resolveEffectiveSealStrategy
	// returns the empty/passphrase default and every explicit value unchanged).
	cfgStrategy := string(resolveEffectiveSealStrategy(cfg, nil))
	threshold := cfg.Seal.Threshold
	totalShares := cfg.Seal.TotalShares

	// (1) Explicit operator override short-circuits the marker lookup.
	if operatorOverride {
		return qrdbsdk.StrategyID(cfgStrategy), threshold, totalShares, false, nil
	}

	// (2) Consult the persisted init marker.
	marker, err := readInitMarker(cfg.Storage.DataDir)
	if err != nil {
		return "", 0, 0, false, err
	}
	if marker != nil && marker.BarrierStrategy != "" {
		strategy := normalizeBarrierStrategy(marker.BarrierStrategy)
		if strategy == qrdbsdk.StrategyShamir {
			if marker.Threshold > 0 {
				threshold = marker.Threshold
			}
			if marker.Shares > 0 {
				totalShares = marker.Shares
			}
		}
		return strategy, threshold, totalShares, true, nil
	}

	// (3) No marker, no override: fall back to the config default.
	return qrdbsdk.StrategyID(cfgStrategy), threshold, totalShares, false, nil
}

// normalizeBarrierStrategy maps the init CLI's strategy aliases onto the
// canonical qrdb StrategyID. The init flag accepts "disabled" as a synonym for
// "none"; every other value passes through verbatim (validated upstream by the
// init flag validator and again by the qrdb strategy builder).
func normalizeBarrierStrategy(strategy string) qrdbsdk.StrategyID {
	if strategy == "disabled" {
		return qrdbsdk.StrategyNone
	}
	return qrdbsdk.StrategyID(strategy)
}
