package remote

import (
	"net"
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
	ln, err := net.Listen("unix", stale)
	if err != nil {
		t.Fatal(err)
	}
	ln.Close() // the steep that listened is gone: its socket file stays
	if n, _ := Send(root, Order{Focus: "x"}); n != 0 {
		t.Fatal("a dead socket got the order")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("the dead socket is left")
	}
}

func TestSendKeepsFiles(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	decoy := filepath.Join(Dir(), Key(root)+".1.sock")
	if err := os.WriteFile(decoy, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, _ := Send(root, Order{Focus: "x"}); n != 0 {
		t.Fatal("a file got the order")
	}
	if _, err := os.Lstat(decoy); err != nil {
		t.Fatal("Send removed a file that is not a socket")
	}
}

func TestSecureDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	dir := Dir()

	// pre-created with loose permissions: taken back, not fatal
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := secureDir(); err != nil {
		t.Fatalf("loose dir: %v", err)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Fatalf("perm = %v", fi.Mode().Perm())
	}

	// a symlink in place of the directory is not trusted
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := secureDir(); err == nil {
		t.Fatal("a symlinked dir was accepted")
	}
}
