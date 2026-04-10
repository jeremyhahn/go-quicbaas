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

package orchestration

import (
	"errors"
	"testing"
)

func TestContainerRuntimeNotAvailableError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &ContainerRuntimeNotAvailableError{Message: "docker not found"}
		want := "container: runtime not available: docker not found"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &ContainerRuntimeNotAvailableError{Message: "test"}
		if !errors.Is(err, ErrContainerRuntimeNotAvailable) {
			t.Error("errors.Is should match ErrContainerRuntimeNotAvailable")
		}
	})

	t.Run("NotOtherSentinel", func(t *testing.T) {
		err := &ContainerRuntimeNotAvailableError{Message: "test"}
		if errors.Is(err, ErrContainerStart) {
			t.Error("should not match ErrContainerStart")
		}
	})

	t.Run("As", func(t *testing.T) {
		var target *ContainerRuntimeNotAvailableError
		err := error(&ContainerRuntimeNotAvailableError{Message: "test msg"})
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Message != "test msg" {
			t.Errorf("Message = %q, want %q", target.Message, "test msg")
		}
	})
}

func TestContainerStartError(t *testing.T) {
	t.Run("ErrorWithCause", func(t *testing.T) {
		cause := &CommandError{Output: "exit code 125"}
		err := &ContainerStartError{Image: "nginx:latest", Cause: cause}
		want := "container: start failed: nginx:latest: exit code 125"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("ErrorWithoutCause", func(t *testing.T) {
		err := &ContainerStartError{Image: "nginx:latest"}
		want := "container: start failed: nginx:latest"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		cause := &CommandError{Output: "fail"}
		err := &ContainerStartError{Image: "test", Cause: cause}
		if !errors.Is(err, ErrContainerStart) {
			t.Error("errors.Is should match ErrContainerStart")
		}
		var cmdErr *CommandError
		if !errors.As(err, &cmdErr) {
			t.Error("errors.As should find CommandError in cause")
		}
	})

	t.Run("UnwrapNoCause", func(t *testing.T) {
		err := &ContainerStartError{Image: "test"}
		errs := err.Unwrap()
		if len(errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(errs))
		}
	})
}

func TestContainerStopError(t *testing.T) {
	t.Run("ErrorWithCause", func(t *testing.T) {
		cause := &CommandError{Output: "timeout"}
		err := &ContainerStopError{ContainerID: "abc123", Cause: cause}
		want := "container: stop failed: abc123: timeout"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("ErrorWithoutCause", func(t *testing.T) {
		err := &ContainerStopError{ContainerID: "abc123"}
		want := "container: stop failed: abc123"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &ContainerStopError{ContainerID: "abc", Cause: &CommandError{Output: "x"}}
		if !errors.Is(err, ErrContainerStop) {
			t.Error("errors.Is should match ErrContainerStop")
		}
	})

	t.Run("UnwrapNoCause", func(t *testing.T) {
		err := &ContainerStopError{ContainerID: "abc"}
		errs := err.Unwrap()
		if len(errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(errs))
		}
	})
}

func TestContainerNotFoundError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &ContainerNotFoundError{ContainerID: "missing-123"}
		want := "container: not found: missing-123"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &ContainerNotFoundError{ContainerID: "x"}
		if !errors.Is(err, ErrContainerNotFound) {
			t.Error("errors.Is should match ErrContainerNotFound")
		}
	})

	t.Run("NotOtherSentinel", func(t *testing.T) {
		err := &ContainerNotFoundError{ContainerID: "x"}
		if errors.Is(err, ErrContainerStop) {
			t.Error("should not match ErrContainerStop")
		}
	})

	t.Run("As", func(t *testing.T) {
		var target *ContainerNotFoundError
		err := error(&ContainerNotFoundError{ContainerID: "abc"})
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.ContainerID != "abc" {
			t.Errorf("ContainerID = %q, want %q", target.ContainerID, "abc")
		}
	})
}

func TestContainerImagePullError(t *testing.T) {
	t.Run("ErrorWithCause", func(t *testing.T) {
		cause := &CommandError{Output: "manifest unknown"}
		err := &ContainerImagePullError{Image: "private/image:v1", Cause: cause}
		want := "container: image pull failed: private/image:v1: manifest unknown"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("ErrorWithoutCause", func(t *testing.T) {
		err := &ContainerImagePullError{Image: "private/image:v1"}
		want := "container: image pull failed: private/image:v1"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &ContainerImagePullError{Image: "test", Cause: &CommandError{Output: "x"}}
		if !errors.Is(err, ErrContainerImagePull) {
			t.Error("errors.Is should match ErrContainerImagePull")
		}
	})

	t.Run("UnwrapNoCause", func(t *testing.T) {
		err := &ContainerImagePullError{Image: "test"}
		errs := err.Unwrap()
		if len(errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(errs))
		}
	})
}

func TestContainerInteractiveStartError(t *testing.T) {
	t.Run("ErrorWithOperationAndCause", func(t *testing.T) {
		cause := &CommandError{Output: "pipe broken"}
		err := &ContainerInteractiveStartError{
			Image:     "quicbaas-agent:latest",
			Operation: "stdin-pipe",
			Cause:     cause,
		}
		want := "container: interactive start failed: quicbaas-agent:latest: stdin-pipe: pipe broken"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("ErrorWithOperationNoCause", func(t *testing.T) {
		err := &ContainerInteractiveStartError{
			Image:     "quicbaas-agent:latest",
			Operation: "start",
		}
		want := "container: interactive start failed: quicbaas-agent:latest: start"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("ErrorImageOnly", func(t *testing.T) {
		err := &ContainerInteractiveStartError{
			Image: "quicbaas-agent:latest",
		}
		want := "container: interactive start failed: quicbaas-agent:latest"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("UnwrapWithCause", func(t *testing.T) {
		cause := &CommandError{Output: "fail"}
		err := &ContainerInteractiveStartError{
			Image:     "test",
			Operation: "start",
			Cause:     cause,
		}
		if !errors.Is(err, ErrContainerInteractiveStart) {
			t.Error("errors.Is should match ErrContainerInteractiveStart")
		}
		var cmdErr *CommandError
		if !errors.As(err, &cmdErr) {
			t.Error("errors.As should find CommandError in cause")
		}
	})

	t.Run("UnwrapNoCause", func(t *testing.T) {
		err := &ContainerInteractiveStartError{Image: "test", Operation: "start"}
		errs := err.Unwrap()
		if len(errs) != 1 {
			t.Errorf("expected 1 error, got %d", len(errs))
		}
	})

	t.Run("NotOtherSentinel", func(t *testing.T) {
		err := &ContainerInteractiveStartError{Image: "test"}
		if errors.Is(err, ErrContainerStart) {
			t.Error("should not match ErrContainerStart")
		}
	})

	t.Run("As", func(t *testing.T) {
		var target *ContainerInteractiveStartError
		err := error(&ContainerInteractiveStartError{
			Image:     "my-image",
			Operation: "stdout-pipe",
		})
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Image != "my-image" {
			t.Errorf("Image = %q, want %q", target.Image, "my-image")
		}
		if target.Operation != "stdout-pipe" {
			t.Errorf("Operation = %q, want %q", target.Operation, "stdout-pipe")
		}
	})
}
