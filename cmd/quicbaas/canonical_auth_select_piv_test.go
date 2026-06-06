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

// Package main: coverage for selectCanonicalPIVReader.
package main

import (
	"testing"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectCanonicalPIVReader_NamedMatch(t *testing.T) {
	readers := []quicbaas.PIVReader{
		{Name: "Yubico"},
		{Name: "ACS"},
	}
	got, err := selectCanonicalPIVReader(readers, "ACS")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "ACS", got.Name)
}

func TestSelectCanonicalPIVReader_NotFound(t *testing.T) {
	readers := []quicbaas.PIVReader{{Name: "Yubico"}}
	_, err := selectCanonicalPIVReader(readers, "NotPresent")
	require.Error(t, err)
	var typed *PIVReaderNotFoundError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "NotPresent", typed.Name)
}

func TestSelectCanonicalPIVReader_NoNameOneReader(t *testing.T) {
	readers := []quicbaas.PIVReader{{Name: "OnlyOne"}}
	got, err := selectCanonicalPIVReader(readers, "")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "OnlyOne", got.Name)
}

func TestSelectCanonicalPIVReader_AmbiguousMultiple(t *testing.T) {
	readers := []quicbaas.PIVReader{
		{Name: "Yubico"},
		{Name: "ACS"},
		{Name: "Identiv"},
	}
	_, err := selectCanonicalPIVReader(readers, "")
	require.Error(t, err)
	var typed *AmbiguousPIVReaderError
	require.ErrorAs(t, err, &typed)
	assert.Len(t, typed.Readers, 3)
}

func TestSelectCanonicalPIVReader_AmbiguousEmptyList(t *testing.T) {
	_, err := selectCanonicalPIVReader(nil, "")
	require.Error(t, err)
	var typed *AmbiguousPIVReaderError
	require.ErrorAs(t, err, &typed)
	assert.Empty(t, typed.Readers)
}
