// Package watcher provides file system watching capabilities.
package watcher

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

// Op represents a file operation type.
type Op int

const (
	Create Op = iota
	Write
	Remove
	Rename
)

func (o Op) String() string {
	switch o {
	case Create:
		return "create"
	case Write:
		return "write"
	case Remove:
		return "remove"
	case Rename:
		return "rename"
	default:
		return "unknown"
	}
}

// Event represents a file system event.
type Event struct {
	Path string
	Op   Op
}

// Watcher watches directories for file changes.
type Watcher interface {
	Start() error
	Stop() error
	Events() <-chan Event
	Errors() <-chan error
	Scan() ([]string, error)
}

// FSWatcher implements Watcher using fsnotify.
type FSWatcher struct {
	paths    []string
	includes []string
	excludes []string
	log      zerolog.Logger

	watcher  *fsnotify.Watcher
	events   chan Event
	errors   chan error
	done     chan struct{}
}

// New creates a new file system watcher.
func New(paths, includes, excludes []string, log zerolog.Logger) (*FSWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &FSWatcher{
		paths:    paths,
		includes: includes,
		excludes: excludes,
		log:      log,
		watcher:  w,
		events:   make(chan Event, 100),
		errors:   make(chan error, 10),
		done:     make(chan struct{}),
	}, nil
}

// Start begins watching the configured paths.
func (w *FSWatcher) Start() error {
	// Add all paths to watcher
	for _, path := range w.paths {
		if err := w.addRecursive(path); err != nil {
			w.log.Warn().Err(err).Str("path", path).Msg("failed to watch path")
		}
	}

	// Start event processing goroutine
	go w.loop()

	return nil
}

func (w *FSWatcher) addRecursive(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := w.watcher.Add(path); err != nil {
				return err
			}
			w.log.Debug().Str("path", path).Msg("watching directory")
		}
		return nil
	})
}

func (w *FSWatcher) loop() {
	defer close(w.events)
	defer close(w.errors)

	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			select {
			case w.errors <- err:
			default:
			}
		}
	}
}

func (w *FSWatcher) handleEvent(event fsnotify.Event) {
	// Skip directories
	info, err := os.Stat(event.Name)
	if err == nil && info.IsDir() {
		// If a new directory was created, watch it
		if event.Op&fsnotify.Create != 0 {
			if err := w.addRecursive(event.Name); err != nil {
				w.log.Warn().Err(err).Str("path", event.Name).Msg("failed to watch new directory")
			}
		}
		return
	}

	// Check if file matches patterns
	if !w.matchesPatterns(event.Name) {
		return
	}

	// Convert fsnotify event to our event type
	var op Op
	switch {
	case event.Op&fsnotify.Create != 0:
		op = Create
	case event.Op&fsnotify.Write != 0:
		op = Write
	case event.Op&fsnotify.Remove != 0:
		op = Remove
	case event.Op&fsnotify.Rename != 0:
		op = Rename
	default:
		return
	}

	select {
	case w.events <- Event{Path: event.Name, Op: op}:
	default:
		w.log.Warn().Str("path", event.Name).Msg("event channel full, dropping event")
	}
}

func (w *FSWatcher) matchesPatterns(path string) bool {
	base := filepath.Base(path)

	// Check excludes first
	for _, pattern := range w.excludes {
		if matched, _ := filepath.Match(pattern, base); matched {
			return false
		}
	}

	// Check includes
	if len(w.includes) == 0 {
		return true
	}
	for _, pattern := range w.includes {
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}
	}

	return false
}

// Stop stops the watcher.
func (w *FSWatcher) Stop() error {
	select {
	case <-w.done:
		// Already closed
		return nil
	default:
		close(w.done)
	}
	return w.watcher.Close()
}

// Events returns the event channel.
func (w *FSWatcher) Events() <-chan Event {
	return w.events
}

// Errors returns the error channel.
func (w *FSWatcher) Errors() <-chan error {
	return w.errors
}

// Scan performs a full directory scan and returns matching files.
func (w *FSWatcher) Scan() ([]string, error) {
	var files []string

	for _, root := range w.paths {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if w.matchesPatterns(path) {
				files = append(files, path)
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

	return files, nil
}

// MatchPattern checks if a filename matches a glob pattern.
func MatchPattern(pattern, name string) bool {
	// Handle ** for recursive matching
	if strings.Contains(pattern, "**") {
		// Simplified handling - just match the base name
		pattern = strings.ReplaceAll(pattern, "**", "*")
	}
	matched, _ := filepath.Match(pattern, name)
	return matched
}
