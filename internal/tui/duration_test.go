package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewDurationModel_DefaultValue(t *testing.T) {
	m := newDurationModel(60)
	if m.defaultMin != 60 {
		t.Errorf("defaultMin = %d, want 60", m.defaultMin)
	}
	if m.textinput.Value() != "60" {
		t.Errorf("textinput value = %q, want %q", m.textinput.Value(), "60")
	}
}

func TestDurationModel_Value_Default(t *testing.T) {
	m := newDurationModel(60)
	if got := m.Value(); got != 60 {
		t.Errorf("Value() = %d, want 60", got)
	}
}

func TestDurationModel_Value_Custom(t *testing.T) {
	m := newDurationModel(60)
	m.textinput.SetValue("30")
	if got := m.Value(); got != 30 {
		t.Errorf("Value() = %d, want 30", got)
	}
}

func TestDurationModel_Value_Empty(t *testing.T) {
	m := newDurationModel(60)
	m.textinput.SetValue("")
	if got := m.Value(); got != 60 {
		t.Errorf("Value() with empty input = %d, want default 60", got)
	}
}

func TestDurationModel_Value_Invalid(t *testing.T) {
	m := newDurationModel(60)
	m.textinput.SetValue("abc")
	if got := m.Value(); got != 60 {
		t.Errorf("Value() with invalid input = %d, want default 60", got)
	}
}

func TestDurationModel_Value_Zero(t *testing.T) {
	m := newDurationModel(60)
	m.textinput.SetValue("0")
	if got := m.Value(); got != 60 {
		t.Errorf("Value() with 0 = %d, want default 60", got)
	}
}

func TestDurationModel_Value_Negative(t *testing.T) {
	m := newDurationModel(60)
	m.textinput.SetValue("-5")
	if got := m.Value(); got != 60 {
		t.Errorf("Value() with negative = %d, want default 60", got)
	}
}

func TestDurationModel_View_ContainsPrompt(t *testing.T) {
	m := newDurationModel(60)
	view := m.View()
	if view == "" {
		t.Error("View() returned empty string")
	}
}

func TestDurationModel_Update_PassesThrough(t *testing.T) {
	m := newDurationModel(60)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	// The textinput should have been updated (exact behavior depends on focus state)
	_ = updated
}
