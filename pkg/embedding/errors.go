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

package embedding

import "errors"

// ErrVectorNotEnabled is returned when vector embedding operations are
// attempted without the vector build tag.
var ErrVectorNotEnabled = errors.New("vector embedding support not enabled: build with -tags vector")

// Configuration validation errors.
var (
	// ErrProviderRequired indicates the provider field is required.
	ErrProviderRequired = errors.New("provider is required")

	// ErrProviderInvalid indicates an unsupported provider was specified.
	ErrProviderInvalid = errors.New("provider must be one of: voyage, ollama, lmstudio, openai")

	// ErrAPIURLRequired indicates the API URL is required.
	ErrAPIURLRequired = errors.New("api_url is required")

	// ErrAPIKeyRequired indicates the API key is required for Voyage AI.
	ErrAPIKeyRequired = errors.New("api_key is required for Voyage AI")

	// ErrModelRequired indicates the model field is required.
	ErrModelRequired = errors.New("model is required")

	// ErrDimensionsPositive indicates dimensions must be positive.
	ErrDimensionsPositive = errors.New("dimensions must be positive")

	// ErrMaxBatchSizePositive indicates max batch size must be positive.
	ErrMaxBatchSizePositive = errors.New("max_batch_size must be positive")

	// ErrTimeoutPositive indicates timeout must be positive.
	ErrTimeoutPositive = errors.New("timeout must be positive")
)
