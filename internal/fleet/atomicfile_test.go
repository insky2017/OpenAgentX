package fleet

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteExactFileAtomicIsPrivateIdempotentAndNonDestructive(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "profile")
	path := filepath.Join(directory, "fleet.yaml")
	created, err := WriteExactFileAtomic(path, []byte("version: 1\n"), AtomicFileOptions{})
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info.Mode().Perm(), err)
	}
	created, err = WriteExactFileAtomic(path, []byte("version: 1\n"), AtomicFileOptions{})
	if err != nil || created {
		t.Fatalf("idempotent write: created=%v err=%v", created, err)
	}
	if _, err := WriteExactFileAtomic(path, []byte("version: 2\n"), AtomicFileOptions{}); !errors.Is(err, ErrFileConflict) {
		t.Fatalf("conflict=%v", err)
	}
	content, _ := os.ReadFile(path)
	if string(content) != "version: 1\n" {
		t.Fatalf("conflict changed old file: %q", content)
	}
}

func TestWriteExactFileAtomicFailureKeepsOldFileAndCleansTemporary(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "worker.yaml")
	failure := errors.New("injected crash")
	_, err := WriteExactFileAtomic(path, []byte("worker\n"), AtomicFileOptions{BeforeRename: func() error { return failure }})
	if !errors.Is(err, failure) {
		t.Fatalf("failure=%v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed write installed target: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary files remain: entries=%v err=%v", entries, err)
	}
}

func TestWriteExactFileAtomicRejectsSymlinkAndBroadPermissions(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(directory, "link")
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteExactFileAtomic(symlink, []byte("same"), AtomicFileOptions{}); err == nil {
		t.Fatal("symlink accepted")
	}
	broad := filepath.Join(directory, "broad")
	if err := os.WriteFile(broad, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteExactFileAtomic(broad, []byte("same"), AtomicFileOptions{}); err == nil {
		t.Fatal("broad permissions accepted")
	}
}

func TestReadSecureFileUsesOpenedRegularFileAndRejectsUnsafeInputs(t *testing.T) {
	directory := t.TempDir()
	private := filepath.Join(directory, "private")
	if err := os.WriteFile(private, []byte("captured"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := ReadSecureFile(private, SecureFileOptions{MaximumBytes: 32, RequirePrivate: true})
	if err != nil || string(content) != "captured" {
		t.Fatalf("content=%q err=%v", content, err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(private, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecureFile(link, SecureFileOptions{MaximumBytes: 32}); err == nil {
		t.Fatal("symlink input accepted")
	}
	broad := filepath.Join(directory, "broad")
	if err := os.WriteFile(broad, []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecureFile(broad, SecureFileOptions{MaximumBytes: 32, RequirePrivate: true}); err == nil {
		t.Fatal("broad private file accepted")
	}
	if content, err := ReadSecureFile(broad, SecureFileOptions{MaximumBytes: 32}); err != nil || string(content) != "source" {
		t.Fatalf("explicit broad source content=%q err=%v", content, err)
	}
}
