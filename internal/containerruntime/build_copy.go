// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package containerruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	securejoin "github.com/cyphar/filepath-securejoin"
)

// COPY ownership: a copied or written file keeps the owner the copy gives
// it (the build's own user for a heredoc, which is root in the build
// container) unless --chown names one, resolved against the target root
// filesystem's passwd and group files as a container user is. Modes and
// modification times of copied files are kept; a heredoc is 0644 unless
// --chmod sets it, and missing parent directories are created 0755.

// copyStep runs a COPY step: it writes the heredoc, or copies the sources
// out of the earlier stage's image, into a root filesystem created from
// the image current, and commits it.
func (b *stageBuild) copyStep(ctx context.Context, step *buildStep, current string) (imageRecord, error) {
	if step.FromStage < 0 {
		return b.commitInRoot(ctx, current, copyCreatedBy(step, ""), func(rootfs string) error {
			return writeHeredoc(rootfs, step)
		})
	}
	from := strconv.Itoa(step.FromStage)
	if name := b.plan.Stages[step.FromStage].Name; name != "" {
		from = name
	}
	source := b.stageImages[step.FromStage]
	return b.commitInRoot(ctx, current, copyCreatedBy(step, from), func(rootfs string) error {
		return b.withSourceRoot(ctx, source, func(sourceRoot string) error {
			return copyFromRoot(sourceRoot, rootfs, step)
		})
	})
}

// copyCreatedBy is a COPY step's history entry, as docker history shows it.
func copyCreatedBy(step *buildStep, from string) string {
	parts := []string{"COPY"}
	if from != "" {
		parts = append(parts, "--from="+from)
	}
	if step.Chmod != "" {
		parts = append(parts, "--chmod="+step.Chmod)
	}
	if step.Chown != "" {
		parts = append(parts, "--chown="+step.Chown)
	}
	if from == "" {
		parts = append(parts, "<<heredoc>")
	} else {
		parts = append(parts, step.Sources...)
	}
	return strings.Join(append(parts, step.Dest), " ")
}

// withSourceRoot creates a root filesystem from the image imageID, runs fn
// on it and removes it.
func (b *stageBuild) withSourceRoot(ctx context.Context, imageID string, fn func(rootfs string) error) error {
	r := b.r
	id := "build-from-" + r.generateContainerID()
	rootfs, err := r.backends.rootfs.Create(ctx, id, imageID)
	if err != nil {
		return fmt.Errorf("create the source stage's root filesystem: %w", err)
	}
	err = fn(rootfs)
	if rmErr := r.backends.rootfs.Remove(id); rmErr != nil {
		err = errors.Join(err, rmErr)
	}
	return err
}

// inRoot resolves name, an absolute path in the image, to a path relative
// to rootfs with every symlink in it resolved as the image sees it: an
// absolute link target is taken from rootfs, and nothing leads outside it.
func inRoot(rootfs, name string) (string, error) {
	resolved, err := securejoin.SecureJoin(rootfs, name)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootfs, resolved)
	if err != nil {
		return "", err
	}
	return rel, nil
}

// inRootParent is inRoot for name's parent directory, joined with name's
// last element unresolved, so a symlink at name itself is not followed.
func inRootParent(rootfs, name string) (string, error) {
	clean := path.Clean("/" + name)
	if clean == "/" {
		return ".", nil
	}
	parent, err := inRoot(rootfs, path.Dir(clean))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, path.Base(clean)), nil
}

// fileMode converts an octal mode as COPY --chmod spells it.
func fileMode(octal string) (fs.FileMode, error) {
	bits, err := strconv.ParseUint(octal, 8, 32)
	if err != nil || bits > 0o7777 {
		return 0, fmt.Errorf("%s is not an octal file mode", octal)
	}
	mode := fs.FileMode(bits & 0o777)
	if bits&syscall.S_ISUID != 0 {
		mode |= fs.ModeSetuid
	}
	if bits&syscall.S_ISGID != 0 {
		mode |= fs.ModeSetgid
	}
	if bits&syscall.S_ISVTX != 0 {
		mode |= fs.ModeSticky
	}
	return mode, nil
}

// writeHeredoc writes a COPY heredoc's body into rootfs.
func writeHeredoc(rootfs string, step *buildStep) error {
	mode := fs.FileMode(0o644)
	if step.Chmod != "" {
		var err error
		if mode, err = fileMode(step.Chmod); err != nil {
			return err
		}
	}
	rel, err := inRoot(rootfs, step.Dest)
	if err != nil {
		return err
	}
	if rel == "." {
		return errors.New("COPY heredoc names the root directory")
	}
	root, err := os.OpenRoot(rootfs)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		return err
	}
	file, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(step.Content)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	if err := root.Chmod(rel, mode); err != nil {
		return err
	}
	return chownCopied(root, rootfs, step.Chown, []string{rel})
}

// copyFromRoot copies a COPY --from step's sources out of sourceRoot into
// rootfs. One source onto a destination not ending in "/" is copied to the
// destination itself; several sources, or a destination ending in "/", are
// copied into the directory it names. A directory's contents are copied,
// not the directory itself, as docker does.
func copyFromRoot(sourceRoot, rootfs string, step *buildStep) error {
	src, err := os.OpenRoot(sourceRoot)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenRoot(rootfs)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	sources, err := expandSources(sourceRoot, src, step.Sources)
	if err != nil {
		return err
	}
	intoDir := len(sources) > 1 || strings.HasSuffix(step.Dest, "/")
	dest, err := inRoot(rootfs, step.Dest)
	if err != nil {
		return err
	}
	if intoDir {
		if err := dst.MkdirAll(dest, 0o755); err != nil {
			return err
		}
	} else if err := dst.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	var copied []string
	for _, source := range sources {
		info, err := src.Lstat(source)
		if err != nil {
			return err
		}
		target := dest
		switch {
		case info.IsDir():
			// The directory's contents go to dest.
		case intoDir:
			target = filepath.Join(dest, filepath.Base(source))
		}
		if copied, err = copyEntry(src, source, dst, target, copied); err != nil {
			return err
		}
	}
	return chownCopied(dst, rootfs, step.Chown, copied)
}

// expandSources resolves each source to a path in src, expanding wildcards.
// The source's parent directories are resolved as the image sees them; a
// symlink at the source itself is copied as a symlink.
func expandSources(sourceRoot string, src *os.Root, sources []string) ([]string, error) {
	var out []string
	for _, source := range sources {
		rel, err := inRootParent(sourceRoot, source)
		if err != nil {
			return nil, err
		}
		if !strings.ContainsAny(source, `*?[`) {
			out = append(out, rel)
			continue
		}
		matches, err := fs.Glob(src.FS(), filepath.ToSlash(rel))
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("COPY source %s matches nothing", source)
		}
		for _, match := range matches {
			out = append(out, filepath.FromSlash(match))
		}
	}
	return out, nil
}

// copyEntry copies the file, directory tree or symlink name in src to
// target in dst, keeping modes and modification times, and returns copied
// with every path it wrote appended.
func copyEntry(src *os.Root, name string, dst *os.Root, target string, copied []string) ([]string, error) {
	info, err := src.Lstat(name)
	if err != nil {
		return copied, err
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		link, err := src.Readlink(name)
		if err != nil {
			return copied, err
		}
		if err := dst.RemoveAll(target); err != nil {
			return copied, err
		}
		if err := dst.Symlink(link, target); err != nil {
			return copied, err
		}
		return append(copied, target), nil
	case info.IsDir():
		if existing, err := dst.Lstat(target); err != nil || !existing.IsDir() {
			if err := dst.RemoveAll(target); err != nil {
				return copied, err
			}
			if err := dst.Mkdir(target, 0o700); err != nil {
				return copied, err
			}
		}
		entries, err := fs.ReadDir(src.FS(), filepath.ToSlash(name))
		if err != nil {
			return copied, err
		}
		for _, entry := range entries {
			if copied, err = copyEntry(src, filepath.Join(name, entry.Name()), dst, filepath.Join(target, entry.Name()), copied); err != nil {
				return copied, err
			}
		}
	case info.Mode().IsRegular():
		if err := copyRegular(src, name, dst, target); err != nil {
			return copied, err
		}
	default:
		return copied, fmt.Errorf("COPY of %s: %s files are not supported", name, info.Mode().Type())
	}
	if err := dst.Chmod(target, info.Mode()&(fs.ModePerm|fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky)); err != nil {
		return copied, err
	}
	if err := dst.Chtimes(target, info.ModTime(), info.ModTime()); err != nil {
		return copied, err
	}
	return append(copied, target), nil
}

// copyRegular copies the regular file name in src to target in dst, over
// whatever target was.
func copyRegular(src *os.Root, name string, dst *os.Root, target string) error {
	if existing, err := dst.Lstat(target); err == nil && !existing.Mode().IsRegular() {
		if err := dst.RemoveAll(target); err != nil {
			return err
		}
	}
	in, err := src.Open(name)
	if err != nil {
		return err
	}
	out, err := dst.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return errors.Join(err, in.Close())
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close(), in.Close())
}

// chownCopied gives every path in names the owner spec (--chown), resolved
// against rootfs. An empty spec changes nothing.
func chownCopied(root *os.Root, rootfs, spec string, names []string) error {
	if spec == "" {
		return nil
	}
	owner, err := parseUserSpec(spec, rootfs)
	if err != nil {
		return fmt.Errorf("COPY --chown=%s: %w", spec, err)
	}
	for _, name := range names {
		if err := root.Lchown(name, int(owner.UID), int(owner.GID)); err != nil {
			return err
		}
	}
	return nil
}
