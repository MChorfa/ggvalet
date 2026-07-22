package cmd

import (
	"testing"

	"github.com/MChorfa/ggvalet/internal/journal"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

func updateTUI(t *testing.T, m tuiModel, msg tea.Msg) tuiModel {
	t.Helper()
	updated, cmd := m.Update(msg)
	m, ok := updated.(tuiModel)
	if !ok {
		t.Fatal("Update did not return a tuiModel")
	}
	if cmd != nil {
		return updateTUI(t, m, cmd())
	}
	return m
}

func TestTUIHelpersAndModel(t *testing.T) {
	setupTestClientWithServer(t)
	addJournalEntry(t, journal.OpCreate, journal.EntityIssue, "tui issue")

	m := newTUI(cfg.Host, cfg.DefaultProject, cfg.DefaultGroup)

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init did not return a command")
	}
	m = updateTUI(t, m, cmd())
	m = updateTUI(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	if !m.ready {
		t.Fatal("model should be ready after WindowSizeMsg")
	}

	view := m.View()
	if view == "" {
		t.Fatal("View returned empty string")
	}
	_ = m.selectedURL()

	// Switch through all tabs; each unloaded tab triggers an async load.
	m = updateTUI(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	_ = m.View()
	_ = m.selectedURL()

	m = updateTUI(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	_ = m.View()
	_ = m.selectedURL()

	m = updateTUI(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	_ = m.View()
	_ = m.selectedURL()

	// Navigate and reload.
	m = updateTUI(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updateTUI(t, m, tea.KeyMsg{Type: tea.KeyDown, Runes: []rune("down")})
	m = updateTUI(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = updateTUI(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})

	if m.list.FilterState() == list.Filtering {
		t.Fatal("model should not be in filtering state")
	}

	_ = m.View()
	_ = m.selectedURL()
}
