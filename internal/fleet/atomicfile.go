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

func CheckExactFile(path string, content []byte) (bool, error) {
	if !filepath.IsAbs(path) {
		return false, fmt.Errorf("atomic file path must be absolute")
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %q: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return false, fmt.Errorf("%q must be a regular file with permissions no wider than 0600", path)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Geteuid() {
		return false, fmt.Errorf("%q must be owned by the current user", path)
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %q: %w", path, err)
	}
	if bytes.Equal(existing, content) {
		return true, nil
	}
	want, have := sha256.Sum256(content), sha256.Sum256(existing)
	return false, fmt.Errorf("%w: %q (existing sha256 %.12x, requested sha256 %.12x)", ErrFileConflict, path, have, want)
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

func readLimitedFile(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maximum {
		return nil, fmt.Errorf("file exceeds safe size limit")
	}
	return content, nil
}
