package credentials

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFileProviderRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p, err := newFileProvider(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() == "" {
		t.Fatal("provider should have a name")
	}
	ctx := context.Background()
	cred := Credential{
		ConnectionID:  "conn-123",
		Password:      "hunter2",
		PrivateKeyPEM: "-----BEGIN PRIVATE KEY-----\nSECRET\n-----END PRIVATE KEY-----\n",
		KeyPassphrase: "s3cr3t passphrase",
	}
	if err := p.Put(ctx, cred); err != nil {
		t.Fatal(err)
	}
	got, err := p.Get(ctx, "conn-123")
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != cred.Password || got.PrivateKeyPEM != cred.PrivateKeyPEM || got.KeyPassphrase != cred.KeyPassphrase {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	// Files on disk must be encrypted: no plaintext secrets.
	secretsDir := filepath.Join(dir, "secrets")
	entries, err := os.ReadDir(secretsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 { // master.key + conn file
		t.Fatalf("expected 2 files, got %d", len(entries))
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(secretsDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if containsAll(raw, []byte("hunter2"), []byte("BEGIN PRIVATE"), []byte("s3cr3t")) {
			t.Errorf("plaintext secret found in %s", e.Name())
		}
		// POSIX permission bits are only meaningful on Unix-like systems.
		// On Windows the OS ignores them (NTFS ACLs apply instead), so the
		// 0600 assertion is only enforced there; encryption and
		// round-trip guarantees are checked on every platform above.
		if runtime.GOOS != "windows" {
			info, _ := e.Info()
			if info.Mode().Perm() != 0o600 {
				t.Errorf("%s has perms %o, want 0600", e.Name(), info.Mode().Perm())
			}
		}
	}

	// Delete.
	if err := p.Delete(ctx, "conn-123"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(ctx, "conn-123"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func containsAll(haystack []byte, needles ...[]byte) bool {
	for _, n := range needles {
		if !contains(haystack, n) {
			return false
		}
	}
	return true
}

func contains(haystack, needle []byte) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			match := true
			for j := range needle {
				if haystack[i+j] != needle[j] {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
		return false
	})()
}

func TestFileProviderTamperDetection(t *testing.T) {
	dir := t.TempDir()
	p, _ := newFileProvider(dir)
	ctx := context.Background()
	_ = p.Put(ctx, Credential{ConnectionID: "c1", Password: "pw"})
	// Corrupt the encrypted blob.
	enc := filepath.Join(dir, "secrets", "c1.enc")
	raw, _ := os.ReadFile(enc)
	for i := range raw {
		raw[i] ^= 0xff
	}
	_ = os.WriteFile(enc, raw, 0o600)
	if _, err := p.Get(ctx, "c1"); err == nil {
		t.Fatal("decrypting tampered ciphertext should fail (GCM auth)")
	}
}
