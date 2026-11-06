package run

import (
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "r")
	s, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Add(Hit{Status: 200 + i}, []byte("req"), []byte("resp"))
		}()
	}
	wg.Wait()
	if err := s.Close(Meta{Total: 20, Done: 20}); err != nil {
		t.Fatal(err)
	}
	m, hits, err := Load(dir)
	if err != nil || m.Total != 20 || len(hits) != 20 {
		t.Fatalf("meta = %+v, %d hits, %v", m, len(hits), err)
	}
	seen := map[int]bool{}
	for _, h := range hits {
		seen[h.N] = true
	}
	if len(seen) != 20 || !seen[1] || !seen[20] {
		t.Fatalf("numbers = %v", seen)
	}
	if b, err := ReadResponse(dir, 7); err != nil || string(b) != "resp" {
		t.Fatalf("response = %q, %v", b, err)
	}
	if _, hits, err := Load(filepath.Join(dir, "missing")); err != nil || hits != nil {
		t.Fatalf("missing run: %v, %v", hits, err)
	}
}

func TestLoadLongLine(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "r")
	s, _ := Create(dir)
	long := strings.Repeat("x", 17<<20)
	s.Add(Hit{Input: map[string]string{"§1§": long}}, nil, nil)
	s.Add(Hit{Status: 200}, nil, nil)
	s.Close(Meta{})
	_, hits, err := Load(dir)
	if err != nil || len(hits) != 2 || hits[0].Input["§1§"] != long {
		t.Fatalf("%d hits, %v", len(hits), err)
	}
}

func TestStorePrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "r")
	s, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(Hit{}, []byte("GET / HTTP/1.1\r\n\r\n"), []byte("HTTP/1.1 200 OK\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(Meta{}); err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Errorf("%s: %o", p, perm)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestForget(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "r")
	s, _ := Create(dir)
	for range 4 {
		s.Add(Hit{Status: 200}, []byte("req"), []byte("resp"))
	}
	s.Close(Meta{Total: 4, Done: 4})
	SaveMarks(dir, map[int]bool{2: true, 3: true})
	if err := Forget(dir, []int{1, 3}); err != nil {
		t.Fatal(err)
	}
	_, hits, err := Load(dir)
	if err != nil || len(hits) != 2 || hits[0].N != 2 || hits[1].N != 4 {
		t.Fatalf("hits = %+v, %v", hits, err)
	}
	if marks := Marks(dir); len(marks) != 1 || !marks[2] {
		t.Fatalf("marks = %v", marks)
	}
	if _, err := ReadResponse(dir, 3); err == nil {
		t.Fatal("dump of a forgotten hit kept")
	}
	if _, err := ReadRequest(dir, 4); err != nil {
		t.Fatal(err)
	}
}
