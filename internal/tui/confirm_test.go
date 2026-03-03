package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewConfirmApp_InitialState(t *testing.T) {
	m := NewConfirmApp("test message")
	if m.cursor != 0 {
		t.Errorf("initial cursor = %d, want 0", m.cursor)
	}
	if m.quitting {
		t.Error("expected quitting to be false initially")
	}
	if m.GetResult() != nil {
		t.Error("expected nil result before interaction")
	}
}

func TestConfirmApp_EnterConfirms(t *testing.T) {
	m := NewConfirmApp("test message")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model := updated.(*confirmModel)
	if model.GetResult() == nil {
		t.Fatal("expected result after enter")
	}
	if !model.GetResult().Confirmed {
		t.Error("expected confirmed=true when cursor is on first option")
	}
	if cmd == nil {
		t.Error("expected quit command")
	}
}

func TestConfirmApp_DownThenEnterCancels(t *testing.T) {
	m := NewConfirmApp("test message")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model := updated.(*confirmModel)
	if model.GetResult() == nil {
		t.Fatal("expected result after enter")
	}
	if model.GetResult().Confirmed {
		t.Error("expected confirmed=false when cursor is on Cancel")
	}
}

func TestConfirmApp_EscCancels(t *testing.T) {
	m := NewConfirmApp("test message")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model := updated.(*confirmModel)
	if model.GetResult() == nil {
		t.Fatal("expected result after esc")
	}
	if model.GetResult().Confirmed {
		t.Error("expected confirmed=false on esc")
	}
}

func TestConfirmApp_CtrlCCancels(t *testing.T) {
	m := NewConfirmApp("test message")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model := updated.(*confirmModel)
	if model.GetResult() == nil {
		t.Fatal("expected result after ctrl+c")
	}
	if model.GetResult().Confirmed {
		t.Error("expected confirmed=false on ctrl+c")
	}
}

func TestConfirmApp_CursorBounds(t *testing.T) {
	m := NewConfirmApp("test message")
	// Move up from 0 — should stay at 0
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	model := updated.(*confirmModel)
	if model.cursor != 0 {
		t.Errorf("cursor after up from 0 = %d, want 0", model.cursor)
	}

	// Move down twice — should stop at 1 (only 2 options)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(*confirmModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(*confirmModel)
	if model.cursor != 1 {
		t.Errorf("cursor after 2x down = %d, want 1", model.cursor)
	}
}

func TestConfirmApp_ViewNotEmpty(t *testing.T) {
	m := NewConfirmApp("test message")
	view := m.View()
	if view == "" {
		t.Error("View() returned empty string")
	}
}

func TestConfirmApp_ViewEmptyWhenQuitting(t *testing.T) {
	m := NewConfirmApp("test message")
	m.quitting = true
	view := m.View()
	if view != "" {
		t.Errorf("View() when quitting = %q, want empty", view)
	}
}

func TestConfirmApp_JKNavigation(t *testing.T) {
	m := NewConfirmApp("test message")
	// j moves down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model := updated.(*confirmModel)
	if model.cursor != 1 {
		t.Errorf("cursor after j = %d, want 1", model.cursor)
	}
	// k moves up
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	model = updated.(*confirmModel)
	if model.cursor != 0 {
		t.Errorf("cursor after k = %d, want 0", model.cursor)
	}
}
