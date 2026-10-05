package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Index is an incremental metadata cache: one entry per session file, keyed
// by path and invalidated by size + mtime. Think of it as CDC on your own
// history: only files that changed since the last run get parsed again.
type Index struct {
	path    string
	Entries map[string]*entry `json:"entries"`
	dirty   bool
}

type entry struct {
	Size     int64      `json:"size"`
	ModNano  int64      `json:"mod"`
	Sessions []*Session `json:"sessions"`
}

const indexVersion = "v1"

func OpenIndex() *Index {
	ix := &Index{path: filepath.Join(CacheDir(), "index-"+indexVersion+".json"), Entries: map[string]*entry{}}
	if b, err := os.ReadFile(ix.path); err == nil {
		json.Unmarshal(b, ix)
		if ix.Entries == nil {
			ix.Entries = map[string]*entry{}
		}
	}
	return ix
}

// Stats about the last Build, shown by `aictl doctor`.
type Stats struct {
	Files, Parsed, Cached, Failed int
	Errors                        []string
}

// Build scans every source and returns all sessions, newest first.
func (ix *Index) Build(sources []Source) ([]*Session, map[string]Source, Stats) {
	var st Stats
	var mu sync.Mutex
	var all []*Session
	owner := map[string]Source{}
	seen := map[string]bool{}

	type job struct {
		src  Source
		path string
	}
	var jobs []job
	for _, src := range sources {
		files, err := src.Files()
		if err != nil {
			st.Errors = append(st.Errors, src.Name()+": "+err.Error())
			continue
		}
		for _, f := range files {
			jobs = append(jobs, job{src, f})
		}
	}
	st.Files = len(jobs)

	work := make(chan job)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range work {
				fi, err := os.Stat(j.path)
				if err != nil {
					continue
				}
				key := j.src.Name() + "|" + j.path
				mu.Lock()
				e := ix.Entries[key]
				mu.Unlock()
				var ss []*Session
				if e != nil && e.Size == fi.Size() && e.ModNano == fi.ModTime().UnixNano() {
					ss = e.Sessions
					mu.Lock()
					st.Cached++
					mu.Unlock()
				} else {
					ss, err = j.src.Parse(j.path)
					mu.Lock()
					if err != nil {
						st.Failed++
						if len(st.Errors) < 20 {
							st.Errors = append(st.Errors, j.src.Name()+": "+filepath.Base(j.path)+": "+err.Error())
						}
					} else {
						st.Parsed++
						ix.Entries[key] = &entry{Size: fi.Size(), ModNano: fi.ModTime().UnixNano(), Sessions: ss}
						ix.dirty = true
					}
					mu.Unlock()
				}
				mu.Lock()
				for _, s := range ss {
					if s.Messages == 0 {
						continue
					}
					k := s.Tool + "/" + s.ID
					if seen[k] {
						continue
					}
					seen[k] = true
					all = append(all, s)
					owner[k] = j.src
				}
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		work <- j
	}
	close(work)
	wg.Wait()

	// forget files that no longer exist
	live := map[string]bool{}
	for _, j := range jobs {
		live[j.src.Name()+"|"+j.path] = true
	}
	for k := range ix.Entries {
		if !live[k] {
			delete(ix.Entries, k)
			ix.dirty = true
		}
	}
	Sort(all)
	return all, owner, st
}

func (ix *Index) Save() error {
	if !ix.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(ix.path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(ix)
	if err != nil {
		return err
	}
	tmp := ix.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, ix.path)
}

// Key identifies a session across tools.
func Key(s *Session) string { return s.Tool + "/" + s.ID }
