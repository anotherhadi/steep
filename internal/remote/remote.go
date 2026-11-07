package remote

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// Order is what a line asks.
type Order struct {
	Focus string `json:"focus,omitempty"`
}

// Dir is where the sockets are.
func Dir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "steep")
	}
	return filepath.Join(os.TempDir(), "steep-"+strconv.Itoa(os.Getuid()))
}

// Key names the sockets of the collection at root.
func Key(root string) string {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return hex.EncodeToString(sum[:8])
}

type Listener struct {
	ln     net.Listener
	path   string
	orders chan Order
}

// Listen opens this steep's socket for the collection at root.
func Listen(root string) (*Listener, error) {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, Key(root)+"."+strconv.Itoa(os.Getpid())+".sock")
	_ = os.Remove(path) // left by a steep that had this pid
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	l := &Listener{ln: ln, path: path, orders: make(chan Order, 16)}
	go l.serve()
	return l, nil
}

func (l *Listener) serve() {
	defer close(l.orders)
	for {
		c, err := l.ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			continue
		}
		go l.read(c)
	}
}

func (l *Listener) read(c net.Conn) {
	defer c.Close()
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 4096), 1<<16)
	for sc.Scan() {
		var o Order
		if json.Unmarshal(sc.Bytes(), &o) == nil && o.Focus != "" {
			select {
			case l.orders <- o:
			default: // steep is busy: the order is not worth blocking for
			}
		}
	}
}

// Orders delivers what is asked, until Close.
func (l *Listener) Orders() <-chan Order { return l.orders }

func (l *Listener) Close() error {
	err := l.ln.Close()
	_ = os.Remove(l.path)
	return err
}

// Send gives o to every steep open on the collection at root, and tells how
// many got it. Sockets nobody listens on any more are removed.
func Send(root string, o Order) (int, error) {
	b, err := json.Marshal(o)
	if err != nil {
		return 0, err
	}
	paths, _ := filepath.Glob(filepath.Join(Dir(), Key(root)+".*.sock"))
	n := 0
	for _, p := range paths {
		c, err := net.Dial("unix", p)
		if err != nil {
			_ = os.Remove(p)
			continue
		}
		if _, err := c.Write(append(b, '\n')); err == nil {
			n++
		}
		c.Close()
	}
	return n, nil
}
