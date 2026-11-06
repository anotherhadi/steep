package collection

import (
	"io/fs"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch reports changes under requests/, made by steep or by anything else,
// at most once per debounce period. Stop it with stop.
func (c *Collection) Watch(debounce time.Duration) (changes <-chan struct{}, stop func(), err error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, err
	}
	addTree := func(root string) {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = w.Add(p)
			}
			return nil
		})
	}
	addTree(c.requestsDir())

	out := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		var timer <-chan time.Time
		for {
			select {
			case <-done:
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if ev.Has(fsnotify.Create) {
					addTree(ev.Name)
				}
				if timer == nil {
					timer = time.After(debounce)
				}
			case <-w.Errors:
			case <-timer:
				timer = nil
				select {
				case out <- struct{}{}:
				default:
				}
			}
		}
	}()
	return out, func() { close(done); w.Close() }, nil
}
