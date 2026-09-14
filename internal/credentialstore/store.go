package credentialstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
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
	credential.SocketPath = filepath.Clean(credential.SocketPath)
	if err := validateCredential(credential); err != nil {
		return err
	}
	doc, err := s.read(true)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if doc.Current == nil {
		doc.Current = make(map[string]selection)
	}
	replaced := false
	for index := range doc.Credentials {
		candidate := doc.Credentials[index]
		if candidate.SocketPath == credential.SocketPath && candidate.InstallationID == credential.InstallationID && candidate.Username == credential.Username {
			doc.Credentials[index] = credential
			replaced = true
			break
		}
	}
	if !replaced {
		doc.Credentials = append(doc.Credentials, credential)
	}
	doc.Version = 1
	doc.Current[credential.SocketPath] = selection{InstallationID: credential.InstallationID, Username: credential.Username}
	return s.write(doc)
}

func (s *Store) Load(socketPath, installationID, username string) (Credential, error) {
	doc, err := s.read(false)
	if err != nil {
		return Credential{}, err
	}
	socketPath = filepath.Clean(socketPath)
	if username == "" {
		selected, ok := doc.Current[socketPath]
		if !ok || selected.InstallationID != installationID {
			return Credential{}, ErrNotFound
		}
		username = selected.Username
	}
	for _, credential := range doc.Credentials {
		if credential.SocketPath == socketPath && credential.InstallationID == installationID && credential.Username == username {
			return credential, nil
		}
	}
	return Credential{}, ErrNotFound
}

func (s *Store) LoadCurrentForSocket(socketPath string) (Credential, error) {
	doc, err := s.read(false)
	if err != nil {
		return Credential{}, err
	}
	socketPath = filepath.Clean(socketPath)
	selected, ok := doc.Current[socketPath]
	if !ok {
		return Credential{}, ErrNotFound
	}
	for _, credential := range doc.Credentials {
		if credential.SocketPath == socketPath && credential.InstallationID == selected.InstallationID && credential.Username == selected.Username {
			return credential, nil
		}
	}
	return Credential{}, ErrNotFound
}

func (s *Store) Delete(credential Credential) (bool, error) {
	doc, err := s.read(false)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	kept := doc.Credentials[:0]
	removed := false
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
		return false, nil
	}
	if len(doc.Credentials) == 0 {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("remove CLI credential store: %w", err)
		}
		if err := syncDirectory(filepath.Dir(s.path)); err != nil {
			return false, err
		}
		return true, nil
	}
	return true, s.write(doc)
}

func (s *Store) read(allowMissing bool) (document, error) {
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
	var doc document
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil || doc.Version != 1 {
		return document{}, fmt.Errorf("%w: invalid credential document", ErrUnsafeCredential)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return document{}, fmt.Errorf("%w: credential document has trailing data", ErrUnsafeCredential)
	}
	if doc.Current == nil {
		doc.Current = make(map[string]selection)
	}
	for _, credential := range doc.Credentials {
		if err := validateCredential(credential); err != nil {
			return document{}, err
		}
	}
	return doc, nil
}

func (s *Store) write(doc document) error {
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
	if !filepath.IsAbs(credential.SocketPath) || strings.TrimSpace(credential.InstallationID) == "" ||
		strings.TrimSpace(credential.Username) == "" || strings.TrimSpace(credential.TokenID) == "" ||
		strings.TrimSpace(credential.Token) == "" || credential.AbsoluteExpires.IsZero() {
		return fmt.Errorf("%w: credential metadata is incomplete", ErrUnsafeCredential)
	}
	return nil
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
