package credentialstore

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"
)

const (
	maxCredentialDocumentBytes = 1 << 20
	maxCredentialEntries       = 128
)

var (
	ErrNotFound         = errors.New("CLI credential not found")
	ErrUnsafeCredential = errors.New("unsafe CLI credential store")
)

type Credential struct {
	SocketPath      string    `json:"socket_path"`
	InstallationID  string    `json:"installation_id"`
	Username        string    `json:"username"`
	TokenID         string    `json:"token_id"`
	Token           string    `json:"token"`
	AbsoluteExpires time.Time `json:"absolute_expires_at"`
}

type selection struct {
	InstallationID string `json:"installation_id"`
	Username       string `json:"username"`
}

type document struct {
	Version     int                  `json:"version"`
	Credentials []Credential         `json:"credentials"`
	Current     map[string]selection `json:"current"`
}

type Options struct {
	CurrentUID   func() int
	BeforeRename func() error
}

type Store struct {
	path         string
	currentUID   func() int
	beforeRename func() error
}

func New(path string, options Options) (*Store, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: credential path must be absolute", ErrUnsafeCredential)
	}
	if options.CurrentUID == nil {
		options.CurrentUID = os.Geteuid
	}
	return &Store{path: filepath.Clean(path), currentUID: options.CurrentUID, beforeRename: options.BeforeRename}, nil
}

func (s *Store) Save(credential Credential) error {
	return s.withExclusiveLock(true, func() error {
		return s.saveUnlocked(credential)
	})
}

// Replace serializes remote token replacement and the local read-modify-write
// transaction so a slower concurrent login cannot restore a token revoked by
// a later login.
func (s *Store) Replace(issue func() (Credential, error)) (Credential, error) {
	if issue == nil {
		return Credential{}, fmt.Errorf("CLI credential issuer is required")
	}
	var credential Credential
	err := s.withExclusiveLock(true, func() error {
		issued, err := issue()
		credential = issued
		if err != nil {
			return err
		}
		return s.saveUnlocked(issued)
	})
	return credential, err
}

func (s *Store) Load(socketPath, installationID, username string) (Credential, error) {
	if err := validateLookup(socketPath, installationID, username, true); err != nil {
		return Credential{}, err
	}
	var result Credential
	err := s.withExclusiveLock(false, func() error {
		doc, err := s.readUnlocked(false)
		if err != nil {
			return err
		}
		if username == "" {
			selected, ok := doc.Current[socketPath]
			if !ok || selected.InstallationID != installationID {
				return ErrNotFound
			}
			username = selected.Username
		}
		for _, credential := range doc.Credentials {
			if credential.SocketPath == socketPath && credential.InstallationID == installationID && credential.Username == username {
				result = credential
				return nil
			}
		}
		return ErrNotFound
	})
	return result, err
}

func (s *Store) LoadCurrentForSocket(socketPath string) (Credential, error) {
	if err := validateSocketPath(socketPath); err != nil {
		return Credential{}, err
	}
	var result Credential
	err := s.withExclusiveLock(false, func() error {
		doc, err := s.readUnlocked(false)
		if err != nil {
			return err
		}
		selected, ok := doc.Current[socketPath]
		if !ok {
			return ErrNotFound
		}
		for _, credential := range doc.Credentials {
			if credential.SocketPath == socketPath && credential.InstallationID == selected.InstallationID && credential.Username == selected.Username {
				result = credential
				return nil
			}
		}
		return ErrNotFound
	})
	return result, err
}

func (s *Store) Delete(credential Credential) (bool, error) {
	if err := validateCredential(credential); err != nil {
		return false, err
	}
	removed := false
	err := s.withExclusiveLock(false, func() error {
		doc, err := s.readUnlocked(false)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		kept := doc.Credentials[:0]
		for _, candidate := range doc.Credentials {
			if candidate.SocketPath == credential.SocketPath && candidate.InstallationID == credential.InstallationID && candidate.Username == credential.Username {
				removed = true
				continue
			}
			kept = append(kept, candidate)
		}
		doc.Credentials = kept
		if selected, ok := doc.Current[credential.SocketPath]; ok && selected.InstallationID == credential.InstallationID && selected.Username == credential.Username {
			delete(doc.Current, credential.SocketPath)
		}
		if !removed {
			return nil
		}
		if len(doc.Credentials) == 0 {
			if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove CLI credential store: %w", err)
			}
			return syncDirectory(filepath.Dir(s.path))
		}
		return s.writeUnlocked(doc)
	})
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return removed, err
}

func (s *Store) saveUnlocked(credential Credential) error {
	if err := validateCredential(credential); err != nil {
		return err
	}
	doc, err := s.readUnlocked(true)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if doc.Current == nil {
		doc.Current = make(map[string]selection)
	}
	kept := doc.Credentials[:0]
	for _, candidate := range doc.Credentials {
		if candidate.SocketPath == credential.SocketPath && candidate.Username == credential.Username {
			continue
		}
		kept = append(kept, candidate)
	}
	doc.Credentials = append(kept, credential)
	doc.Version = 1
	doc.Current[credential.SocketPath] = selection{InstallationID: credential.InstallationID, Username: credential.Username}
	if err := validateDocument(doc); err != nil {
		return err
	}
	return s.writeUnlocked(doc)
}

func (s *Store) readUnlocked(allowMissing bool) (document, error) {
	if err := s.inspectDirectory(allowMissing); err != nil {
		return document{}, err
	}
	info, err := os.Lstat(s.path)
	if os.IsNotExist(err) {
		return document{Version: 1, Current: make(map[string]selection)}, ErrNotFound
	}
	if err != nil {
		return document{}, fmt.Errorf("inspect CLI credential store: %w", err)
	}
	if err := s.validateOwnedMode(info, false); err != nil {
		return document{}, err
	}
	fd, err := syscall.Open(s.path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return document{}, fmt.Errorf("open CLI credential store: %w", err)
	}
	file := os.NewFile(uintptr(fd), s.path)
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return document{}, fmt.Errorf("inspect opened CLI credential store: %w", err)
	}
	if err := s.validateOwnedMode(openedInfo, false); err != nil {
		return document{}, err
	}
	encoded, err := io.ReadAll(io.LimitReader(file, maxCredentialDocumentBytes+1))
	if err != nil {
		return document{}, fmt.Errorf("read CLI credential store: %w", err)
	}
	if len(encoded) > maxCredentialDocumentBytes {
		return document{}, fmt.Errorf("%w: credential document is too large", ErrUnsafeCredential)
	}
	var doc document
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil || doc.Version != 1 {
		return document{}, fmt.Errorf("%w: invalid credential document", ErrUnsafeCredential)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return document{}, fmt.Errorf("%w: credential document has trailing data", ErrUnsafeCredential)
	}
	if err := validateDocument(doc); err != nil {
		return document{}, err
	}
	return doc, nil
}

func (s *Store) writeUnlocked(doc document) error {
	if err := validateDocument(doc); err != nil {
		return err
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create CLI credential directory: %w", err)
	}
	if err := s.inspectDirectory(false); err != nil {
		return err
	}
	if info, err := os.Lstat(s.path); err == nil {
		if err := s.validateOwnedMode(info, false); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect CLI credential store: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".credentials-*")
	if err != nil {
		return fmt.Errorf("create temporary CLI credential: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	failed := true
	defer func() {
		if failed {
			_ = temporary.Close()
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("secure temporary CLI credential: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(doc); err != nil {
		return fmt.Errorf("encode CLI credential store: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary CLI credential: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary CLI credential: %w", err)
	}
	if s.beforeRename != nil {
		if err := s.beforeRename(); err != nil {
			return fmt.Errorf("replace CLI credential store: %w", err)
		}
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace CLI credential store: %w", err)
	}
	failed = false
	if err := syncDirectory(directory); err != nil {
		return err
	}
	return nil
}

func (s *Store) withExclusiveLock(createDirectory bool, operation func() error) error {
	directory := filepath.Dir(s.path)
	if createDirectory {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create CLI credential directory: %w", err)
		}
	}
	if err := s.inspectDirectory(false); err != nil {
		return err
	}
	lockPath := s.path + ".lock"
	if info, err := os.Lstat(lockPath); err == nil {
		if err := s.validateLockFile(info); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect CLI credential lock: %w", err)
	}
	fd, err := syscall.Open(lockPath, syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("open CLI credential lock: %w", err)
	}
	lock := os.NewFile(uintptr(fd), lockPath)
	defer lock.Close()
	info, err := lock.Stat()
	if err != nil {
		return fmt.Errorf("inspect opened CLI credential lock: %w", err)
	}
	if err := s.validateLockFile(info); err != nil {
		return err
	}
	for {
		err = syscall.Flock(fd, syscall.LOCK_EX)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("lock CLI credential store: %w", err)
	}
	operationErr := operation()
	unlockErr := syscall.Flock(fd, syscall.LOCK_UN)
	if operationErr != nil {
		return operationErr
	}
	if unlockErr != nil {
		return fmt.Errorf("unlock CLI credential store: %w", unlockErr)
	}
	return nil
}

func (s *Store) validateLockFile(info os.FileInfo) error {
	if err := s.validateOwnedMode(info, false); err != nil {
		return err
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("%w: credential lock permissions must be 0600", ErrUnsafeCredential)
	}
	return nil
}

func (s *Store) inspectDirectory(allowMissing bool) error {
	info, err := os.Lstat(filepath.Dir(s.path))
	if os.IsNotExist(err) {
		if allowMissing {
			return nil
		}
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("inspect CLI credential directory: %w", err)
	}
	return s.validateOwnedMode(info, true)
}

func (s *Store) validateOwnedMode(info os.FileInfo, directory bool) error {
	if info.Mode()&os.ModeSymlink != 0 || (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return fmt.Errorf("%w: path type is not allowed", ErrUnsafeCredential)
	}
	allowed := os.FileMode(0o600)
	if directory {
		allowed = 0o700
	}
	if info.Mode().Perm()&^allowed != 0 {
		return fmt.Errorf("%w: permissions are too broad", ErrUnsafeCredential)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != s.currentUID() {
		return fmt.Errorf("%w: path owner does not match current user", ErrUnsafeCredential)
	}
	return nil
}

func validateCredential(credential Credential) error {
	if validateSocketPath(credential.SocketPath) != nil || !validMetadata(credential.InstallationID) ||
		!validMetadata(credential.Username) || !validMetadata(credential.TokenID) || !validToken(credential.Token) ||
		credential.AbsoluteExpires.IsZero() || credential.AbsoluteExpires.Before(time.Unix(0, 0)) {
		return fmt.Errorf("%w: credential metadata is incomplete", ErrUnsafeCredential)
	}
	return nil
}

func validateDocument(doc document) error {
	if doc.Version != 1 || len(doc.Credentials) > maxCredentialEntries || len(doc.Current) > maxCredentialEntries {
		return fmt.Errorf("%w: invalid credential document", ErrUnsafeCredential)
	}
	type credentialKey struct{ socket, installation, username string }
	credentials := make(map[credentialKey]struct{}, len(doc.Credentials))
	for _, credential := range doc.Credentials {
		if err := validateCredential(credential); err != nil {
			return err
		}
		key := credentialKey{credential.SocketPath, credential.InstallationID, credential.Username}
		if _, duplicate := credentials[key]; duplicate {
			return fmt.Errorf("%w: duplicate credential entry", ErrUnsafeCredential)
		}
		credentials[key] = struct{}{}
	}
	if doc.Current == nil {
		doc.Current = make(map[string]selection)
	}
	for socketPath, selected := range doc.Current {
		if validateSocketPath(socketPath) != nil || !validMetadata(selected.InstallationID) || !validMetadata(selected.Username) {
			return fmt.Errorf("%w: invalid current credential selection", ErrUnsafeCredential)
		}
		if _, ok := credentials[credentialKey{socketPath, selected.InstallationID, selected.Username}]; !ok {
			return fmt.Errorf("%w: dangling current credential selection", ErrUnsafeCredential)
		}
	}
	return nil
}

func validateLookup(socketPath, installationID, username string, allowEmptyUsername bool) error {
	if err := validateSocketPath(socketPath); err != nil {
		return err
	}
	if !validMetadata(installationID) || (!allowEmptyUsername || username != "") && !validMetadata(username) {
		return fmt.Errorf("%w: credential lookup metadata is invalid", ErrUnsafeCredential)
	}
	return nil
}

func validateSocketPath(path string) error {
	if strings.TrimSpace(path) != path || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("%w: credential socket path must be canonical and absolute", ErrUnsafeCredential)
	}
	return nil
}

func validMetadata(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 256 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validToken(token string) bool {
	if strings.TrimSpace(token) != token || token == "" {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(raw) >= 32 && len(raw) <= 128
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open CLI credential directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync CLI credential directory: %w", err)
	}
	return nil
}
