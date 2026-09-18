// Package protocoltest provides a shared in-memory protocol.Client test
// double. Test code in multiple packages (manager, transfers, server) uses
// it so the fixture is written once; it is a test support package, not
// product code.
package protocoltest

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"

	"remorasftp/internal/config"
	"remorasftp/internal/protocol"
)

// Node is one in-memory filesystem node.
type Node struct {
	IsDir   bool
	Data    []byte
	Symlink bool
}

// FakeFS is an in-memory protocol.Client backed by a path map. It is the
// test double for the shared protocol layer: everything the manager and the
// transfer engine do (list/stat/mkdir/remove/rename/upload/download) runs
// through it.
type FakeFS struct {
	mu     sync.Mutex
	nodes  map[string]*Node
	closed bool
}

// NewFakeFS creates a fake filesystem containing only the root directory.
func NewFakeFS() *FakeFS {
	return &FakeFS{nodes: map[string]*Node{"/": {IsDir: true}}}
}

func (f *FakeFS) ensureParent(p string) {
	dir := path.Dir(p)
	for dir != "/" && dir != "." {
		if _, ok := f.nodes[dir]; !ok {
			f.nodes[dir] = &Node{IsDir: true}
		}
		dir = path.Dir(dir)
	}
}

// Add inserts a file or directory (parents are created as needed).
func (f *FakeFS) Add(p string, isDir bool, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureParent(p)
	f.nodes[p] = &Node{IsDir: isDir, Data: data}
}

// Read returns the raw content of a file node (or nil).
func (f *FakeFS) Read(p string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n, ok := f.nodes[path.Clean(p)]; ok {
		return n.Data
	}
	return nil
}

// Has reports whether a path exists in the fake tree.
func (f *FakeFS) Has(p string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.nodes[path.Clean(p)]
	return ok
}

func (f *FakeFS) Connect(context.Context) error { return nil }

func (f *FakeFS) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func (f *FakeFS) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		Protocol: config.ProtoSFTP, Encrypted: true,
		UnixPermissions: true, Chmod: true, Symlinks: true,
		ServerSideRename: true, MaxPreviewBytes: 1 << 20,
	}
}

func (f *FakeFS) ServerInfo() protocol.ServerInfo {
	return protocol.ServerInfo{Protocol: config.ProtoSFTP, Host: "fake", Port: 22, StartDir: "/", Encrypted: true}
}

func (f *FakeFS) List(_ context.Context, dir string) ([]protocol.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	dir = path.Clean(dir)
	n, ok := f.nodes[dir]
	if !ok || !n.IsDir {
		return nil, errors.New("no such directory: " + dir)
	}
	out := []protocol.Entry{}
	for p, node := range f.nodes {
		if path.Dir(p) != dir || p == dir {
			continue
		}
		e := protocol.Entry{Name: path.Base(p), Path: p}
		switch {
		case node.Symlink:
			e.Type = protocol.EntrySymlink
			e.IsSymlink = true
		case node.IsDir:
			e.Type = protocol.EntryDir
		default:
			e.Type = protocol.EntryFile
			e.Size = int64(len(node.Data))
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *FakeFS) Stat(_ context.Context, p string) (protocol.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p = path.Clean(p)
	n, ok := f.nodes[p]
	if !ok {
		return protocol.Entry{}, errors.New("no such file: " + p)
	}
	e := protocol.Entry{Name: path.Base(p), Path: p}
	switch {
	case n.Symlink:
		e.Type = protocol.EntrySymlink
		e.IsSymlink = true
	case n.IsDir:
		e.Type = protocol.EntryDir
	default:
		e.Type = protocol.EntryFile
		e.Size = int64(len(n.Data))
	}
	return e, nil
}

func (f *FakeFS) Mkdir(_ context.Context, p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p = path.Clean(p)
	if _, ok := f.nodes[p]; ok {
		return errors.New("exists: " + p)
	}
	f.ensureParent(p)
	f.nodes[p] = &Node{IsDir: true}
	return nil
}

func (f *FakeFS) Remove(_ context.Context, p string, recursive bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p = path.Clean(p)
	n, ok := f.nodes[p]
	if !ok {
		return errors.New("no such file: " + p)
	}
	if n.IsDir && recursive {
		for q := range f.nodes {
			if q == p || strings.HasPrefix(q, p+"/") {
				delete(f.nodes, q)
			}
		}
		return nil
	}
	if n.IsDir {
		for q := range f.nodes {
			if strings.HasPrefix(q, p+"/") {
				return errors.New("directory not empty: " + p)
			}
		}
	}
	delete(f.nodes, p)
	return nil
}

func (f *FakeFS) Rename(_ context.Context, from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	from, to = path.Clean(from), path.Clean(to)
	if _, ok := f.nodes[from]; !ok {
		return errors.New("no such file: " + from)
	}
	if _, ok := f.nodes[to]; ok {
		return errors.New("exists: " + to)
	}
	// Move the subtree.
	renamed := map[string]*Node{}
	for q, node := range f.nodes {
		if q == from || strings.HasPrefix(q, from+"/") {
			renamed[to+strings.TrimPrefix(q, from)] = node
		}
	}
	for q := range f.nodes {
		if q == from || strings.HasPrefix(q, from+"/") {
			delete(f.nodes, q)
		}
	}
	for k, v := range renamed {
		f.nodes[k] = v
	}
	return nil
}

func (f *FakeFS) Chmod(context.Context, string, os.FileMode) error { return nil }

func (f *FakeFS) Upload(_ context.Context, dst string, r io.Reader, _ int64, _ protocol.ProgressFunc) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureParent(dst)
	f.nodes[path.Clean(dst)] = &Node{Data: data}
	return nil
}

func (f *FakeFS) Download(_ context.Context, src string, w io.Writer, off int64, _ protocol.ProgressFunc) error {
	f.mu.Lock()
	n, ok := f.nodes[path.Clean(src)]
	f.mu.Unlock()
	if !ok || n.IsDir {
		return errors.New("no such file: " + src)
	}
	if off > 0 && off < int64(len(n.Data)) {
		_, err := w.Write(n.Data[off:])
		return err
	}
	_, err := w.Write(n.Data)
	return err
}

func (f *FakeFS) ReadPartial(_ context.Context, p string, max int64) ([]byte, bool, error) {
	f.mu.Lock()
	n, ok := f.nodes[path.Clean(p)]
	f.mu.Unlock()
	if !ok || n.IsDir {
		return nil, false, errors.New("no such file: " + p)
	}
	if int64(len(n.Data)) > max {
		return append([]byte(nil), n.Data[:max]...), true, nil
	}
	return append([]byte(nil), n.Data...), false, nil
}
