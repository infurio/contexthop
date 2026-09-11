package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
)

type workTick struct{ id int }
type workFinished struct {
	id         int
	transition Transition
	choice     Choice
	before     Draft
	process    bool
}

func workingTick(id int) tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return workTick{id: id} })
}

func (m AppModel) startWork(label string, run func(context.Context, func(string)) Transition, choice Choice, before Draft, process bool) (tea.Model, tea.Cmd) {
	// Keep the source page visible while the worker computes a private result.
	m.workBackground = m.View().Content
	ctx, cancel := context.WithCancel(context.Background())
	m.workCompact = m.current().picker.CompactDialog
	m.workStopping = false
	if choice.Option.Cancellable {
		m.workCancel = cancel
	}
	m.working = true
	m.workID++
	m.workStarted, m.workFrame, m.workLabel = time.Now(), 0, label
	id := m.workID
	updates, done := make(chan string, 1), make(chan struct{})
	report := func(label string) {
		select {
		case updates <- label:
		default:
		}
	}
	return m, tea.Batch(workingTick(id), func() tea.Msg {
		defer close(done)
		defer cancel()
		return workFinished{id: id, transition: run(ctx, report), choice: choice, before: before, process: process}
	}, waitWorkProgress(id, updates, done))
}

type workProgress struct {
	id      int
	label   string
	updates <-chan string
	done    <-chan struct{}
}

func waitWorkProgress(id int, updates <-chan string, done <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		select {
		case label := <-updates:
			return workProgress{id, label, updates, done}
		case <-done:
			return nil
		}
	}
}
