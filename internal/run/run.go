// Package run keeps what a fuzzing run found: one line of metadata per hit
// in hits.jsonl, and the raw request and response of each under dumps/.
package run

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"
)

type Hit struct {
	N           int               `json:"n"` // order of arrival, from 1
	Position    int               `json:"position"`
	Input       map[string]string `json:"input"`
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Status      int               `json:"status"`
	Length      int64             `json:"length"`
	Words       int64             `json:"words"`
	Lines       int64             `json:"lines"`
	ContentType string            `json:"content_type,omitempty"`
	Duration    time.Duration     `json:"duration"`
	Redirect    string            `json:"redirect,omitempty"`
}

type Meta struct {
	URL      string    `json:"url"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Total    int       `json:"total"`
	Done     int       `json:"done"`
	Errors   int       `json:"errors"`
	Stopped  bool      `json:"stopped"`
}

// Store writes a run as it goes.
type Store struct {
	Dir string

	mu   sync.Mutex
	hits *os.File
	enc  *json.Encoder
	n    int
}

// Create starts a run in dir, replacing the one there.
func Create(dir string) (*Store, error) {
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "dumps"), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "hits.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	return &Store{Dir: dir, hits: f, enc: json.NewEncoder(f)}, nil
}

// Add numbers h and keeps it with its raw request and response. It is safe
// for concurrent use.
func (s *Store) Add(h Hit, req, resp []byte) (Hit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	h.N = s.n
	base := filepath.Join(s.Dir, "dumps", strconv.Itoa(h.N))
	if err := os.WriteFile(base+".req", req, 0o600); err != nil {
		return h, err
	}
	if err := os.WriteFile(base+".resp", resp, 0o600); err != nil {
		return h, err
	}
	return h, s.enc.Encode(h)
}

func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

func (s *Store) Close(m Meta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.hits.Close()
	b, jerr := json.MarshalIndent(m, "", "  ")
	if jerr != nil {
		return errors.Join(err, jerr)
	}
	return errors.Join(err, os.WriteFile(filepath.Join(s.Dir, "run.json"), b, 0o600))
}

// Load reads the run kept in dir; a missing run is no error, and no hits.
func Load(dir string) (Meta, []Hit, error) {
	var m Meta
	if b, err := os.ReadFile(filepath.Join(dir, "run.json")); err == nil {
		if err := json.Unmarshal(b, &m); err != nil {
			return m, nil, fmt.Errorf("run.json: %w", err)
		}
	}
	f, err := os.Open(filepath.Join(dir, "hits.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return m, nil, nil
	}
	if err != nil {
		return m, nil, err
	}
	defer f.Close()
	var hits []Hit
	// A hit's inputs are as long as their wordlists' lines, so lines are
	// big; a ceiling still keeps a corrupted hits.jsonl from exhausting
	// memory.
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var h Hit
		if json.Unmarshal(sc.Bytes(), &h) == nil {
			hits = append(hits, h)
		} // else a line cut short by a crash
	}
	return m, hits, sc.Err()
}

func ReadRequest(dir string, n int) ([]byte, error) {
	return os.ReadFile(filepath.Join(dir, "dumps", strconv.Itoa(n)+".req"))
}

func ReadResponse(dir string, n int) ([]byte, error) { return os.ReadFile(ResponsePath(dir, n)) }

// Forget drops hits from the run kept in dir: their line in hits.jsonl, their
// dumps and their mark. The others keep their number. Not while the run
// still writes.
func Forget(dir string, ns []int) error {
	gone := make(map[int]bool, len(ns))
	for _, n := range ns {
		gone[n] = true
	}
	_, hits, err := Load(dir)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for _, h := range hits {
		if !gone[h.N] {
			if err := enc.Encode(h); err != nil {
				return err
			}
		}
	}
	tmp := filepath.Join(dir, "hits.jsonl.tmp")
	if err := os.WriteFile(tmp, b.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, "hits.jsonl")); err != nil {
		os.Remove(tmp)
		return err
	}
	marks := Marks(dir)
	var errs []error
	for n := range gone {
		delete(marks, n)
		for _, p := range []string{filepath.Join(dir, "dumps", strconv.Itoa(n)+".req"), ResponsePath(dir, n)} {
			if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(append(errs, SaveMarks(dir, marks))...)
}

// ResponsePath is the file holding hit n's response.
func ResponsePath(dir string, n int) string {
	return filepath.Join(dir, "dumps", strconv.Itoa(n)+".resp")
}

// Marks are the hits marked in the run kept in dir, by number.
func Marks(dir string) map[int]bool {
	var ns []int
	b, _ := os.ReadFile(filepath.Join(dir, "marks.json"))
	_ = json.Unmarshal(b, &ns)
	out := make(map[int]bool, len(ns))
	for _, n := range ns {
		out[n] = true
	}
	return out
}

func SaveMarks(dir string, marks map[int]bool) error {
	ns := make([]int, 0, len(marks))
	for n, ok := range marks {
		if ok {
			ns = append(ns, n)
		}
	}
	slices.Sort(ns)
	b, _ := json.Marshal(ns)
	return os.WriteFile(filepath.Join(dir, "marks.json"), b, 0o600)
}
