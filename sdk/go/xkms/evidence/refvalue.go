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

package evidence

import xkmsevidence "github.com/jeremyhahn/go-xkms/sdk/go/evidence"

// Signed reference values (CoRIM). These re-export pkg/attestation/refvalue:
// VerifySignedCoRIM verifies a COSE_Sign1 signed CoRIM against a pinned key
// or a certificate chain and returns its reference-value triples.

// CoRIMVerifyOptions selects the signer trust source and verification time.
type CoRIMVerifyOptions = xkmsevidence.CoRIMVerifyOptions

// ReferenceManifest is a verified CoRIM's identity and reference values.
type ReferenceManifest = xkmsevidence.ReferenceManifest

// ReferenceValue is one reference-value triple.
type ReferenceValue = xkmsevidence.ReferenceValue

// ReferenceEnvironment identifies a reference value's target environment.
type ReferenceEnvironment = xkmsevidence.ReferenceEnvironment

// ReferenceMeasurement is one expected measurement.
type ReferenceMeasurement = xkmsevidence.ReferenceMeasurement

// MeasurementKey is a measurement's key (mkey).
type MeasurementKey = xkmsevidence.MeasurementKey

// ReferenceDigest is one expected digest with its IANA hash identifier.
type ReferenceDigest = xkmsevidence.ReferenceDigest

// Measurement key kinds.
const (
	MeasurementKeyNone                = xkmsevidence.MeasurementKeyNone
	MeasurementKeyUint                = xkmsevidence.MeasurementKeyUint
	MeasurementKeyOID                 = xkmsevidence.MeasurementKeyOID
	MeasurementKeyUUID                = xkmsevidence.MeasurementKeyUUID
	MeasurementKeyPSARefValID         = xkmsevidence.MeasurementKeyPSARefValID
	MeasurementKeyCCAPlatformConfigID = xkmsevidence.MeasurementKeyCCAPlatformConfigID
)

// VerifySignedCoRIM verifies a COSE_Sign1 signed CoRIM and returns its
// reference values.
var VerifySignedCoRIM = xkmsevidence.VerifySignedCoRIM

// CoRIM verification errors, matched with errors.Is.
var (
	ErrCoRIMTrustSource     = xkmsevidence.ErrCoRIMTrustSource
	ErrCoRIMMalformed       = xkmsevidence.ErrCoRIMMalformed
	ErrCoRIMSignerChain     = xkmsevidence.ErrCoRIMSignerChain
	ErrCoRIMSignature       = xkmsevidence.ErrCoRIMSignature
	ErrCoRIMNotYetValid     = xkmsevidence.ErrCoRIMNotYetValid
	ErrCoRIMExpired         = xkmsevidence.ErrCoRIMExpired
	ErrCoRIMPCRIndex        = xkmsevidence.ErrCoRIMPCRIndex
	ErrCoRIMDigestLength    = xkmsevidence.ErrCoRIMDigestLength
	ErrCoRIMUnsupportedHash = xkmsevidence.ErrCoRIMUnsupportedHash
)
