// Package localfs exposes the local disk through the same protocol.Entry
// vocabulary used by the remote adapters, backing the dual-pane "Local"
// side of the file manager.
//
// Security model: the local API is loopback-only and token-protected, so
// this service is reachable exclusively by the user's own browser tabs on
// this device - the same trust assumption the rest of the engine makes
// about "the user's machine, operated by the user". All paths are cleaned;
// NUL bytes and empty paths are rejected.
package localfs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"remorasftp/internal/protocol"
)

// Service is the local disk file service.
type Service struct{}

// New constructs a Service.
func New() *Service { return &Service{} }

var (
	ErrInvalidPath = errors.New("invalid local path")
	ErrEmptyPath   = errors.New("local path is empty")
)

// clean validates and normalizes a local path.
func clean(p string) (string, error) {
	if p == "" {
		return "", ErrEmptyPath
	}
	if strings.ContainsRune(p, 0) {
		return "", ErrInvalidPath
	}
	return filepath.Clean(p), nil
}

// Home returns the user's home directory (the default local root).
func (s *Service) Home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

func entryFromInfo(name, full string, info os.FileInfo) protocol.Entry {
	e := protocol.Entry{
		Name:     name,
		Path:     full,
		ModTime:  info.ModTime(),
		MIMEType: protocol.GuessMIME(name),
	}
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		e.Type = protocol.EntrySymlink
		e.IsSymlink = true
		if tgt, err := os.Readlink(full); err == nil {
			e.LinkTarget = tgt
		}
	case mode.IsDir():
		e.Type = protocol.EntryDir
	case mode.IsRegular():
		e.Type = protocol.EntryFile
		e.Size = info.Size()
	default:
		e.Type = protocol.EntryOther
	}
	if runtime.GOOS != "windows" {
		perm := uint32(mode.Perm())
		e.Mode = perm
		e.Permissions = protocol.PermString(perm)
	}
	return e
}

// List returns the entries of a local directory.
func (s *Service) List(p string) ([]protocol.Entry, error) {
	p, err := clean(p)
	if err != nil {
		return nil, err
	}
	infos, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.Entry, 0, len(infos))
	for _, d := range infos {
		info, err := d.Info()
		if err != nil {
			continue
		}
		out = append(out, entryFromInfo(d.Name(), filepath.Join(p, d.Name()), info))
	}
	return out, nil
}

// Stat returns the entry for a local path.
func (s *Service) Stat(p string) (protocol.Entry, error) {
	p, err := clean(p)
	if err != nil {
		return protocol.Entry{}, err
	}
	info, err := os.Lstat(p)
	if err != nil {
		return protocol.Entry{}, err
	}
	return entryFromInfo(filepath.Base(p), p, info), nil
}

// Mkdir creates a local directory.
func (s *Service) Mkdir(p string) error {
	p, err := clean(p)
	if err != nil {
		return err
	}
	base := filepath.Base(p)
	if base == "" || base == "." || base == ".." {
		return ErrInvalidPath
	}
	return os.Mkdir(p, 0o755)
}

// Remove deletes a local file or directory (recursive when asked).
func (s *Service) Remove(p string, recursive bool) error {
	p, err := clean(p)
	if err != nil {
		return err
	}
	if recursive {
		return os.RemoveAll(p)
	}
	return os.Remove(p)
}

// Rename moves or renames a local path.
func (s *Service) Rename(from, to string) error {
	from, err := clean(from)
	if err != nil {
		return err
	}
	to, err = clean(to)
	if err != nil {
		return err
	}
	return os.Rename(from, to)
}

// Read opens a local file for streaming; the caller must close.
func (s *Service) Read(p string) (*os.File, int64, error) {
	p, err := clean(p)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

// Base returns the final element of a local path.
func Base(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Base(filepath.Clean(p))
}

// JoinLocal joins a directory and a name into a validated absolute local
// path.
func JoinLocal(dir, name string) (string, error) {
	if dir == "" || name == "" {
		return "", ErrEmptyPath
	}
	d, err := clean(dir)
	if err != nil {
		return "", err
	}
	n, err := clean(name)
	if err != nil {
		return "", err
	}
	if n == "." || n == ".." || strings.Contains(n, string(filepath.Separator)) {
		return "", ErrInvalidPath
	}
	return filepath.Join(d, n), nil
}

// Write streams a local file from r (created fresh, replacing existing).
func (s *Service) Write(p string, r io.Reader) error {
	p, err := clean(p)
	if err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(p)
		return err
	}
	return f.Close()
}

// Copy copies or moves a local tree, honoring the shared conflict policy.
// Guard: the destination may not be inside the source.
func (s *Service) Copy(from, to string, move bool, policy protocol.CopyPolicy) error {
	from, err := clean(from)
	if err != nil {
		return err
	}
	to, err = clean(to)
	if err != nil {
		return err
	}
	if isWithin(from, to) {
		return fmt.Errorf("destination is inside the source: cannot copy a folder into itself")
	}
	// Destination parent must exist.
	if dir := filepath.Dir(to); dir != to {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return fmt.Errorf("destination folder does not exist: %s", dir)
		}
	}
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return s.copyDir(from, to, move, policy)
	}
	return s.copyFile(from, to, move, policy)
}

func (s *Service) copyFile(from, to string, move bool, policy protocol.CopyPolicy) error {
	if policy == "" {
		policy = protocol.PolicyRefuse
	}
	_, err := os.Lstat(to)
	exists := err == nil
	if exists {
		dstInfo, serr := os.Stat(to)
		if serr == nil && dstInfo.IsDir() {
			if policy == protocol.PolicySkip {
				return nil
			}
			return fmt.Errorf("destination is a folder: %s", to)
		}
	}
	switch policy {
	case protocol.PolicySkip:
		if exists {
			return nil
		}
	case protocol.PolicyReplace:
		// overwrite
	case protocol.PolicyRefuse:
		if exists {
			return fmt.Errorf("destination already exists: %s", to)
		}
	case protocol.PolicyRename:
		if exists {
			to = uniqueLocalDest(to)
		}
	}
	if move {
		if err := os.Rename(from, to); err == nil {
			return nil
		} else if !isCrossDevice(err) {
			return err
		}
		// Cross-device: copy then remove.
		if cerr := s.streamFile(from, to); cerr != nil {
			return cerr
		}
		return os.Remove(from)
	}
	return s.streamFile(from, to)
}

func isCrossDevice(err error) bool {
	if err == nil {
		return false
	}
	if le, ok := err.(*os.LinkError); ok {
		err = le.Err
	}
	return strings.Contains(err.Error(), "cross-device")
}

func (s *Service) streamFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(to)
		return err
	}
	info, ierr := os.Stat(from)
	if cerr := out.Close(); ierr == nil && cerr != nil {
		return cerr
	}
	if ierr == nil && info != nil {
		_ = os.Chmod(to, info.Mode().Perm())
	}
	return nil
}

func (s *Service) copyDir(from, to string, move bool, policy protocol.CopyPolicy) error {
	dstInfo, err := os.Lstat(to)
	if err == nil && !dstInfo.IsDir() {
		// Destination is a file.
		switch policy {
		case protocol.PolicySkip:
			return nil
		case protocol.PolicyRename:
			to = uniqueLocalDest(to)
			if err := os.Mkdir(to, 0o755); err != nil {
				return err
			}
		default:
			return fmt.Errorf("cannot replace a file with a folder: %s", to)
		}
	} else if err != nil {
		if err := os.Mkdir(to, 0o755); err != nil {
			return err
		}
	}
	// err == nil && is dir → merge into existing directory (to unchanged).

	infos, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, d := range infos {
		srcChild := filepath.Join(from, d.Name())
		dstChild := filepath.Join(to, d.Name())
		childInfo, err := d.Info()
		if err != nil {
			continue
		}
		if childInfo.IsDir() && childInfo.Mode()&os.ModeSymlink == 0 {
			if err := s.copyDir(srcChild, dstChild, move, policy); err != nil {
				return err
			}
		} else if childInfo.Mode()&os.ModeSymlink != 0 {
			// Never follow local symlinks during tree copy.
			continue
		} else {
			if err := s.copyFile(srcChild, dstChild, move, policy); err != nil {
				return err
			}
		}
	}
	if move {
		if err := os.RemoveAll(from); err != nil {
			return err
		}
	}
	return nil
}

func uniqueLocalDest(to string) string {
	dir := filepath.Dir(to)
	base := filepath.Base(to)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		cand := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", name, i, ext))
		if _, err := os.Lstat(cand); err != nil {
			return cand
		}
	}
}

// isWithin reports whether child is equal to root or strictly inside it.
func isWithin(root, child string) bool {
	root = filepath.Clean(root)
	child = filepath.Clean(child)
	if child == root {
		return true
	}
	return strings.HasPrefix(child, root+string(filepath.Separator))
}
