package fleet

import (
	"bytes"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

// AddAgent preserves existing entries and rejects conflicting ownership/configuration.
// The lock covers read, conflict checks and atomic replacement so concurrent adds
// cannot lose another Agent. Existing exact entries are idempotent.
func AddAgent(path string, entry Agent) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("Fleet manifest must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := validatePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	fd, err := unix.Open(path+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err = unix.Flock(fd, unix.LOCK_EX); err != nil {
		return err
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	manifest := Manifest{Version: ManifestVersion, Session: SessionName}
	original, err := ReadSecureFile(path, SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true})
	if err == nil {
		manifest, err = Decode(bytes.NewReader(original))
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, existing := range manifest.Agents {
		if existing.AgentID == entry.AgentID {
			if existing.WorkerConfig != entry.WorkerConfig || (existing.IdentityFile != "" && existing.IdentityFile != entry.IdentityFile) {
				return fmt.Errorf("%w: Agent %q already has different paths", ErrFileConflict, entry.AgentID)
			}
			return nil
		}
	}
	manifest.Agents = append(manifest.Agents, entry)
	encoded, err := Encode(manifest)
	if err != nil {
		return err
	}
	if original == nil {
		_, err = WriteExactFileAtomic(path, encoded, AtomicFileOptions{})
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".fleet-add-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(encoded); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	current, err := ReadSecureFile(path, SecureFileOptions{MaximumBytes: 1 << 20, RequirePrivate: true})
	if err != nil {
		return err
	}
	if !bytes.Equal(current, original) {
		return fmt.Errorf("%w: Fleet changed during add", ErrFileConflict)
	}
	if err = os.Rename(temp.Name(), path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
