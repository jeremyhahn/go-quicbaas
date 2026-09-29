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

package orchestration

import (
	"errors"
	"strings"
)

// Sentinel errors for the image lifecycle (container_image.go).
var (
	// ErrImageBuild indicates an image could not be built.
	ErrImageBuild = errors.New("container: image build failed")

	// ErrImageInspect indicates a local image could not be inspected.
	ErrImageInspect = errors.New("container: image inspect failed")

	// ErrImageNotFound indicates this host holds no image by that reference.
	ErrImageNotFound = errors.New("container: image not found")

	// ErrImageTag indicates an image could not be tagged.
	ErrImageTag = errors.New("container: image tag failed")

	// ErrImagePush indicates an image could not be pushed to its registry.
	ErrImagePush = errors.New("container: image push failed")

	// ErrRegistryLogin indicates a registry refused the push credential.
	ErrRegistryLogin = errors.New("container: registry login failed")
)

// ImageBuildError indicates an image could not be built.
type ImageBuildError struct {
	Tags  []string
	Cause error
}

// Error implements the error interface.
func (e *ImageBuildError) Error() string {
	return withCause(ErrImageBuild.Error()+": "+strings.Join(e.Tags, ","), e.Cause)
}

// Unwrap returns the sentinel and the cause for errors.Is/As.
func (e *ImageBuildError) Unwrap() []error { return sentinelAndCause(ErrImageBuild, e.Cause) }

// ImageInspectError indicates a local image could not be inspected.
type ImageInspectError struct {
	Image string
	Cause error
}

// Error implements the error interface.
func (e *ImageInspectError) Error() string {
	return withCause(ErrImageInspect.Error()+": "+e.Image, e.Cause)
}

// Unwrap returns the sentinel and the cause for errors.Is/As.
func (e *ImageInspectError) Unwrap() []error { return sentinelAndCause(ErrImageInspect, e.Cause) }

// ImageNotFoundError indicates this host holds no image by that reference.
type ImageNotFoundError struct {
	Image string
}

// Error implements the error interface.
func (e *ImageNotFoundError) Error() string { return ErrImageNotFound.Error() + ": " + e.Image }

// Unwrap returns the sentinel for errors.Is.
func (e *ImageNotFoundError) Unwrap() error { return ErrImageNotFound }

// ImageTagError indicates an image could not be tagged.
type ImageTagError struct {
	Source string
	Target string
	Cause  error
}

// Error implements the error interface.
func (e *ImageTagError) Error() string {
	return withCause(ErrImageTag.Error()+": "+e.Source+" -> "+e.Target, e.Cause)
}

// Unwrap returns the sentinel and the cause for errors.Is/As.
func (e *ImageTagError) Unwrap() []error { return sentinelAndCause(ErrImageTag, e.Cause) }

// ImagePushError indicates an image could not be pushed to its registry.
type ImagePushError struct {
	Image string
	Cause error
}

// Error implements the error interface.
func (e *ImagePushError) Error() string {
	return withCause(ErrImagePush.Error()+": "+e.Image, e.Cause)
}

// Unwrap returns the sentinel and the cause for errors.Is/As.
func (e *ImagePushError) Unwrap() []error { return sentinelAndCause(ErrImagePush, e.Cause) }

// RegistryLoginError indicates a registry refused the push credential.
//
// The credential itself is never part of it: a login reads its secret from
// stdin, so nothing the engine printed can have echoed it from argv.
type RegistryLoginError struct {
	Registry string
	Cause    error
}

// Error implements the error interface.
func (e *RegistryLoginError) Error() string {
	return withCause(ErrRegistryLogin.Error()+": "+e.Registry, e.Cause)
}

// Unwrap returns the sentinel and the cause for errors.Is/As.
func (e *RegistryLoginError) Unwrap() []error { return sentinelAndCause(ErrRegistryLogin, e.Cause) }

// withCause appends a cause's message to msg when there is one.
func withCause(msg string, cause error) string {
	if cause != nil {
		return msg + ": " + cause.Error()
	}
	return msg
}

// sentinelAndCause is the Unwrap list for an error carrying a sentinel and an
// optional cause.
func sentinelAndCause(sentinel, cause error) []error {
	if cause != nil {
		return []error{sentinel, cause}
	}
	return []error{sentinel}
}
