package remote

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSendFocus(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	root := t.TempDir()
	l, err := Listen(root)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if n, err := Send(link, Order{Focus: "a/b"}); err != nil || n != 1 {
		t.Fatalf("Send = %d, %v", n, err)
	}
	select {
	case o := <-l.Orders():
		if o.Focus != "a/b" {
			t.Fatalf("got %+v", o)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no order")
	}
	if n, _ := Send(t.TempDir(), Order{Focus: "x"}); n != 0 {
		t.Fatal("another collection got the order")
	}
}

func TestStaleSocket(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(Dir(), Key(root)+".1.sock")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if n, _ := Send(root, Order{Focus: "x"}); n != 0 {
		t.Fatal("a dead socket got the order")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("the dead socket is left")
	}
}
