package fleet

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

var ErrFileConflict = errors.New("existing file content conflicts")

type AtomicFileOptions struct {
	BeforeRename func() error
}

type SecureFileOptions struct {
	MaximumBytes   int64
	RequirePrivate bool
}

func ReadSecureFile(path string, options SecureFileOptions) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("secure file path must be absolute")
	}
	if options.MaximumBytes <= 0 {
		return nil, fmt.Errorf("secure file size limit must be positive")
	}
	file, err := openNoFollow(path)
	if err != nil {
		return nil, fmt.Errorf("open %q without following symlinks: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect opened file %q: %w", path, err)
	}
	if err := validateOpenedRegularFile(path, info, options.RequirePrivate); err != nil {
		return nil, err
	}
	if info.Size() > options.MaximumBytes {
		return nil, fmt.Errorf("%q exceeds safe size limit", path)
	}
	content, err := io.ReadAll(io.LimitReader(file, options.MaximumBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	if int64(len(content)) > options.MaximumBytes {
		return nil, fmt.Errorf("%q exceeds safe size limit", path)
	}
	return content, nil
}

func ValidateExecutableFile(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("OpenAgentX binary path must be absolute")
	}
	file, err := openNoFollow(path)
	if err != nil {
		return fmt.Errorf("open OpenAgentX binary %q without following symlinks: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect opened OpenAgentX binary %q: %w", path, err)
	}
	if err := validateOpenedRegularFile(path, info, false); err != nil {
		return err
	}
	if info.Mode().Perm()&0o100 == 0 {
		return fmt.Errorf("OpenAgentX binary %q must be executable by its owner", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("OpenAgentX binary %q must not be writable by group or others", path)
	}
	return nil
}

func openNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func validateOpenedRegularFile(path string, info os.FileInfo, requirePrivate bool) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%q must be a regular file", path)
	}
	if requirePrivate && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%q must have permissions no wider than 0600", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("%q must be owned by the current user", path)
	}
	return nil
}

func CheckExactFile(path string, content []byte) (bool, error) {
	if !filepath.IsAbs(path) {
		return false, fmt.Errorf("atomic file path must be absolute")
	}
	existing, err := ReadSecureFile(path, SecureFileOptions{MaximumBytes: maxExactFileBytes(len(content)), RequirePrivate: true})
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if bytes.Equal(existing, content) {
		return true, nil
	}
	want, have := sha256.Sum256(content), sha256.Sum256(existing)
	return false, fmt.Errorf("%w: %q (existing sha256 %.12x, requested sha256 %.12x)", ErrFileConflict, path, have, want)
}

func maxExactFileBytes(requested int) int64 {
	const minimum = int64(1 << 20)
	if int64(requested) >= minimum {
		return int64(requested) + 1
	}
	return minimum
}

func WriteExactFileAtomic(path string, content []byte, options AtomicFileOptions) (bool, error) {
	identical, err := CheckExactFile(path, content)
	if err != nil || identical {
		return false, err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return false, fmt.Errorf("create directory for %q: %w", path, err)
	}
	if err := validatePrivateDirectory(directory); err != nil {
		return false, err
	}
	temporary, err := os.CreateTemp(directory, ".openagentx-*.tmp")
	if err != nil {
		return false, fmt.Errorf("create temporary file for %q: %w", path, err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return false, fmt.Errorf("secure temporary file for %q: %w", path, err)
	}
	if _, err := temporary.Write(content); err != nil {
		return false, fmt.Errorf("write temporary file for %q: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		return false, fmt.Errorf("sync temporary file for %q: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return false, fmt.Errorf("close temporary file for %q: %w", path, err)
	}
	if identical, err := CheckExactFile(path, content); err != nil || identical {
		return false, err
	}
	if options.BeforeRename != nil {
		if err := options.BeforeRename(); err != nil {
			return false, fmt.Errorf("before atomic replace of %q: %w", path, err)
		}
	}
	if err := unix.Renameat2(unix.AT_FDCWD, temporaryPath, unix.AT_FDCWD, path, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			if identical, checkErr := CheckExactFile(path, content); checkErr != nil {
				return false, checkErr
			} else if !identical {
				return false, fmt.Errorf("%w: %q changed during atomic install", ErrFileConflict, path)
			}
			return false, nil
		}
		return false, fmt.Errorf("install %q atomically: %w", path, err)
	}
	committed = true
	if err := syncDirectory(directory); err != nil {
		return false, fmt.Errorf("sync directory for %q: %w", path, err)
	}
	return true, nil
}

func validatePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect directory %q: %w", path, err)
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%q must be a directory with permissions no wider than 0700", path)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("%q must be owned by the current user", path)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return err
	}
	return nil
}
