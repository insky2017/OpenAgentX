package secretstore

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
	"openagentx/internal/domain"
)

const hmacKeyName = ".idempotency-hmac-key"

const maxSecretFileSize = 64 << 10

type Store interface {
	NewVersion() string
	PutImmutable(context.Context, string, []byte) error
	CommitImmutable(context.Context, string, []byte, func() error, func(context.Context, string) (bool, error)) error
	Get(context.Context, string) ([]byte, error)
	GetReferenced(context.Context, string, func(context.Context, string) (bool, error)) ([]byte, error)
	ReconcileOrphans(context.Context, func(context.Context, string) (bool, error)) error
	Fingerprint([]byte) string
	DeleteOrphan(context.Context, string) error
}

type FileStore struct {
	root    string
	rootDir *os.File
	key     []byte
}

func Open(root string) (*FileStore, error) {
	trimmed := strings.TrimSpace(root)
	abs, err := filepath.Abs(trimmed)
	if err != nil || trimmed == "" {
		return nil, domain.ErrInvalidInput("network secret directory is required")
	}
	created := false
	if err := os.Mkdir(abs, 0o700); err == nil {
		created = true
	} else if !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("create network secret directory: %w", err)
	}
	dirFD, err := unix.Open(abs, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, domain.ErrInvalidInput("network secret directory must be a non-symlink 0700 directory")
	}
	rootDir := os.NewFile(uintptr(dirFD), abs)
	info, err := rootDir.Stat()
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		_ = rootDir.Close()
		return nil, domain.ErrInvalidInput("network secret directory must be a non-symlink 0700 directory")
	}
	if created {
		if err := syncDirectory(filepath.Dir(abs)); err != nil {
			_ = rootDir.Close()
			return nil, err
		}
	}
	key, err := loadOrCreateKey(rootDir)
	if err != nil {
		_ = rootDir.Close()
		return nil, err
	}
	return &FileStore{root: abs, rootDir: rootDir, key: key}, nil
}

func (s *FileStore) NewVersion() string { return "secret-" + uuid.NewString() }

func (s *FileStore) PutImmutable(_ context.Context, version string, payload []byte) error {
	if err := validateVersion(version); err != nil {
		return err
	}
	if len(payload) == 0 {
		return domain.ErrInvalidInput("network secret payload cannot be empty")
	}
	if len(payload) > maxSecretFileSize {
		return domain.ErrInvalidInput("network secret payload is too large")
	}
	name := version + ".secret"
	if existing, err := readSecureFileAt(s.rootDir, name, maxSecretFileSize); err == nil {
		if len(existing) == len(payload) && subtle.ConstantTimeCompare(existing, payload) == 1 {
			return nil
		}
		return domain.ErrIdempotencyConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmpLeaf := ".tmp-" + uuid.NewString()
	tmpFD, err := unix.Openat(int(s.rootDir.Fd()), tmpLeaf, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("create network secret temporary file: %w", err)
	}
	tmp := os.NewFile(uintptr(tmpFD), tmpLeaf)
	defer unix.Unlinkat(int(s.rootDir.Fd()), tmpLeaf, 0)
	if _, err = tmp.Write(payload); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write network secret: %w", err)
	}
	if err := unix.Renameat2(int(s.rootDir.Fd()), tmpLeaf, int(s.rootDir.Fd()), name, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			existing, readErr := readSecureFileAt(s.rootDir, name, maxSecretFileSize)
			if readErr == nil && len(existing) == len(payload) && subtle.ConstantTimeCompare(existing, payload) == 1 {
				return nil
			}
			if readErr != nil {
				return readErr
			}
			return domain.ErrIdempotencyConflict
		}
		return fmt.Errorf("commit network secret: %w", err)
	}
	return s.rootDir.Sync()
}

// CommitImmutable serializes one secret version across processes while its
// database reference is committed. If the commit fails, the file is removed
// only after the caller proves that no committed row references it.
func (s *FileStore) CommitImmutable(ctx context.Context, version string, payload []byte, commit func() error, referenced func(context.Context, string) (bool, error)) error {
	if commit == nil || referenced == nil {
		return domain.ErrInvalidInput("network secret commit and reference check are required")
	}
	lock, err := s.lockVersion(ctx, version)
	if err != nil {
		return err
	}
	defer unlockFile(lock)
	if err := s.PutImmutable(ctx, version, payload); err != nil {
		return err
	}
	commitErr := commit()
	if commitErr == nil {
		return nil
	}
	inUse, referenceErr := referenced(ctx, version)
	if referenceErr != nil || inUse {
		return commitErr
	}
	if cleanupErr := s.deleteOrphanLocked(version); cleanupErr != nil {
		return errors.Join(commitErr, cleanupErr)
	}
	return commitErr
}

func (s *FileStore) Get(_ context.Context, version string) ([]byte, error) {
	if err := validateVersion(version); err != nil {
		return nil, err
	}
	payload, err := readSecureFileAt(s.rootDir, version+".secret", maxSecretFileSize)
	if errors.Is(err, os.ErrNotExist) {
		return nil, domain.ErrNotFound
	}
	return payload, err
}

func (s *FileStore) GetReferenced(ctx context.Context, version string, referenced func(context.Context, string) (bool, error)) ([]byte, error) {
	if referenced == nil {
		return nil, domain.ErrInvalidInput("network secret reference check is required")
	}
	inUse, err := referenced(ctx, version)
	if err != nil {
		return nil, err
	}
	if !inUse {
		return nil, domain.ErrNotFound
	}
	return s.Get(ctx, version)
}

func (s *FileStore) ReconcileOrphans(ctx context.Context, referenced func(context.Context, string) (bool, error)) error {
	if referenced == nil {
		return domain.ErrInvalidInput("network secret reference check is required")
	}
	dirFD, err := unix.Openat(int(s.rootDir.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(dirFD), s.root)
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".secret") {
			continue
		}
		version := strings.TrimSuffix(name, ".secret")
		if validateVersion(version) != nil {
			continue
		}
		lock, err := s.lockVersion(ctx, version)
		if err != nil {
			return err
		}
		inUse, referenceErr := referenced(ctx, version)
		if referenceErr == nil && !inUse {
			err = s.deleteOrphanLocked(version)
		}
		unlockFile(lock)
		if referenceErr != nil {
			return referenceErr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *FileStore) Fingerprint(payload []byte) string {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *FileStore) DeleteOrphan(ctx context.Context, version string) error {
	lock, err := s.lockVersion(ctx, version)
	if err != nil {
		return err
	}
	defer unlockFile(lock)
	return s.deleteOrphanLocked(version)
}

func (s *FileStore) deleteOrphanLocked(version string) error {
	if err := validateVersion(version); err != nil {
		return err
	}
	name := version + ".secret"
	if _, err := readSecureFileAt(s.rootDir, name, maxSecretFileSize); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := unix.Unlinkat(int(s.rootDir.Fd()), name, 0); err != nil {
		return fmt.Errorf("remove orphan network secret: %w", err)
	}
	return s.rootDir.Sync()
}

func validateVersion(version string) error {
	if err := domain.ValidateOpaqueID("network secret version", version); err != nil {
		return err
	}
	if filepath.Base(version) != version || strings.ContainsAny(version, `/\\`) {
		return domain.ErrInvalidInput("invalid network secret version")
	}
	return nil
}

func loadOrCreateKey(rootDir *os.File) ([]byte, error) {
	for {
		if key, err := readSecureFileAt(rootDir, hmacKeyName, 32); err == nil {
			if len(key) != 32 {
				return nil, domain.ErrInvalidInput("network secret HMAC key is invalid")
			}
			return key, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		key := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, fmt.Errorf("generate network secret HMAC key: %w", err)
		}
		created, err := publishNoReplaceAt(rootDir, hmacKeyName, key)
		if err != nil {
			return nil, fmt.Errorf("publish network secret HMAC key: %w", err)
		}
		if created {
			return key, nil
		}
	}
}

func readSecureFile(path string) ([]byte, error) {
	return readSecureFileBounded(path, maxSecretFileSize)
}

func readSecureFileBounded(path string, limit int64) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return nil, domain.ErrInvalidInput("network secret file must be a non-symlink 0600 regular file")
	}
	if info.Size() < 0 || info.Size() > limit {
		return nil, domain.ErrInvalidInput("network secret file is too large")
	}
	payload, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, domain.ErrInvalidInput("network secret file is too large")
	}
	return payload, nil
}

func readSecureFileAt(rootDir *os.File, name string, limit int64) ([]byte, error) {
	fd, err := unix.Openat(int(rootDir.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return nil, domain.ErrInvalidInput("network secret file must be a non-symlink 0600 regular file")
	}
	if info.Size() < 0 || info.Size() > limit {
		return nil, domain.ErrInvalidInput("network secret file is too large")
	}
	payload, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, domain.ErrInvalidInput("network secret file is too large")
	}
	return payload, nil
}

func publishNoReplaceAt(rootDir *os.File, name string, payload []byte) (bool, error) {
	tmpLeaf := ".tmp-" + uuid.NewString()
	tmpFD, err := unix.Openat(int(rootDir.Fd()), tmpLeaf, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return false, err
	}
	tmp := os.NewFile(uintptr(tmpFD), tmpLeaf)
	defer unix.Unlinkat(int(rootDir.Fd()), tmpLeaf, 0)
	if _, err = tmp.Write(payload); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	if err = unix.Renameat2(int(rootDir.Fd()), tmpLeaf, int(rootDir.Fd()), name, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return false, nil
		}
		return false, err
	}
	return true, rootDir.Sync()
}

func (s *FileStore) lockVersion(ctx context.Context, version string) (*os.File, error) {
	if err := validateVersion(version); err != nil {
		return nil, err
	}
	name := ".lock-" + version
	fd, err := unix.Openat(int(s.rootDir.Fd()), name, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open network secret version lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), name)
	for {
		if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err == nil {
			return file, nil
		} else if !errors.Is(err, unix.EWOULDBLOCK) {
			_ = file.Close()
			return nil, fmt.Errorf("lock network secret version: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func unlockFile(file *os.File) {
	_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
	_ = file.Close()
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}
