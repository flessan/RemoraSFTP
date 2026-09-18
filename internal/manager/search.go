package manager

import (
	"context"
	"path"
	"strings"

	"remorasftp/internal/protocol"
	"remorasftp/internal/safepath"
)

// SearchQuery describes a remote file search. The search runs through the
// protocol client (breadth-first) — the entire filesystem is never loaded
// into the browser.
type SearchQuery struct {
	Path          string // starting directory
	Query         string // name substring (or prefix with StartsWith)
	StartsWith    bool
	Extension     string // ".txt"
	CaseSensitive bool
	IncludeHidden bool
	Recursive     bool
	MaxResults    int // default 1000, capped at 5000
}

const (
	searchDefaultMax = 1000
	searchHardMax    = 5000
	// searchMaxDirs bounds the number of directories a single search may
	// descend into, keeping huge trees from hanging the request.
	searchMaxDirs = 50000
)

// Search runs the query and returns matching entries (paths already
// resolved). It returns what it found so far when ctx is canceled.
func (m *Manager) Search(ctx context.Context, sessionID string, q SearchQuery) (results []protocol.Entry, canceled bool, err error) {
	cl, err := m.Client(sessionID)
	if err != nil {
		return nil, false, err
	}
	start := safepath.Clean(q.Path)
	max := q.MaxResults
	if max <= 0 {
		max = searchDefaultMax
	}
	if max > searchHardMax {
		max = searchHardMax
	}
	if q.Extension != "" && !strings.HasPrefix(q.Extension, ".") {
		q.Extension = "." + q.Extension
	}
	ext := q.Extension
	if !q.CaseSensitive {
		ext = strings.ToLower(ext)
	}

	queue := []string{start}
	dirsVisited := 0
	for len(queue) > 0 {
		if ctx.Err() != nil {
			return results, true, nil
		}
		if len(results) >= max {
			return results, false, nil
		}
		dir := queue[0]
		queue = queue[1:]
		entries, err := cl.List(ctx, dir)
		if err != nil {
			continue // unreadable directory: skip and keep searching
		}
		for _, e := range entries {
			if !q.IncludeHidden && strings.HasPrefix(e.Name, ".") {
				continue
			}
			isDir := e.Type == protocol.EntryDir && !e.IsSymlink
			if isDir && q.Recursive && dirsVisited < searchMaxDirs {
				queue = append(queue, safepath.Join(dir, e.Name))
				dirsVisited++
			}
			if q.matches(e.Name, ext) {
				e.Path = safepath.Join(dir, e.Name)
				results = append(results, e)
				if len(results) >= max {
					return results, false, nil
				}
			}
		}
	}
	if ctx.Err() != nil {
		return results, true, nil
	}
	return results, false, nil
}

func (q SearchQuery) matches(name, ext string) bool {
	if q.Query == "" && q.Extension == "" {
		return false
	}
	if q.Query != "" {
		hay, needle := name, q.Query
		if !q.CaseSensitive {
			hay, needle = strings.ToLower(hay), strings.ToLower(needle)
		}
		if q.StartsWith {
			if !strings.HasPrefix(hay, needle) {
				return false
			}
		} else if !strings.Contains(hay, needle) {
			return false
		}
	}
	if q.Extension != "" {
		// Normalize a missing leading dot so the method is self-contained
		// (Search already normalizes the query extension before calling).
		if ext != "" && !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		e := path.Ext(name)
		if !q.CaseSensitive {
			e = strings.ToLower(e)
		}
		if e != ext {
			return false
		}
	}
	return true
}
