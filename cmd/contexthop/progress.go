package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type validationProgress struct {
	writer   io.Writer
	terminal bool
	started  time.Time
	done     chan struct{}
	stop     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	message  string
	printed  bool
}

func newValidationProgress(writer io.Writer, terminal bool) *validationProgress {
	progress := &validationProgress{
		writer: writer, terminal: terminal, started: time.Now(),
		done: make(chan struct{}), stop: make(chan struct{}),
	}
	if terminal {
		go progress.animate()
	} else {
		close(progress.done)
	}
	return progress
}

func (progress *validationProgress) Update(message string) {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	progress.message = strings.TrimSpace(message)
	if progress.message == "" {
		return
	}
	if progress.terminal {
		progress.renderLocked()
	} else if !progress.printed {
		fmt.Fprintln(progress.writer, progress.message+"…")
		progress.printed = true
	}
}

func (progress *validationProgress) Stop() {
	progress.once.Do(func() { close(progress.stop) })
	<-progress.done
	progress.mu.Lock()
	defer progress.mu.Unlock()
	if progress.terminal && progress.printed {
		fmt.Fprint(progress.writer, "\r\x1b[2K")
	}
}

func (progress *validationProgress) animate() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	defer close(progress.done)
	for {
		select {
		case <-ticker.C:
			progress.mu.Lock()
			if progress.message != "" {
				progress.renderLocked()
			}
			progress.mu.Unlock()
		case <-progress.stop:
			return
		}
	}
}

func (progress *validationProgress) renderLocked() {
	fmt.Fprintf(progress.writer, "\r\x1b[2K%s · %.1fs", progress.message, time.Since(progress.started).Seconds())
	progress.printed = true
}
