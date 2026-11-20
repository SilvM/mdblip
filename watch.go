package main

import (
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounce = 30 * time.Millisecond

// watch watches the parent directory rather than the file itself, because
// editors like nvim save by rename, which would drop a watch on the file.
func watch(path string, notify func()) (stop func(), err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := w.Add(filepath.Dir(abs)); err != nil {
		w.Close()
		return nil, err
	}
	base := filepath.Base(abs)
	go func() {
		var timer *time.Timer
		for ev := range w.Events {
			if filepath.Base(ev.Name) != base || !ev.Has(fsnotify.Write|fsnotify.Create|fsnotify.Rename) {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounce, notify)
		}
	}()
	return func() { w.Close() }, nil
}
