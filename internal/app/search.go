package app

import (
	"fmt"
	"strings"

	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/search"
	tea "github.com/charmbracelet/bubbletea"
)

// searchStore is the part of the repository global search needs.
type searchStore interface {
	GlobalSearch(search.Query, int) ([]search.Result, error)
}

// globalSearchMsg delivers a global-search page back to the update loop.
type globalSearchMsg struct {
	revision uint64
	results  []search.Result
	err      error
}

// openGlobalSearch starts application-wide search across every cached message.
func (m *Model) openGlobalSearch() {
	m.modal = "search-all"
	m.searchInput.SetValue("")
	m.searchInput.CursorEnd()
	m.searchInput.Focus()
	m.globalResults = nil
	m.choice = 0
	m.searchRev++
}

// runGlobalSearch queries the FTS index for the current input.
func (m *Model) runGlobalSearch() tea.Cmd {
	s, ok := m.store.(searchStore)
	if !ok {
		return nil
	}
	q := search.Parse(m.searchInput.Value())
	if q.Empty() {
		m.globalResults = nil
		m.choice = 0
		return nil
	}
	m.searchRev++
	rev := m.searchRev
	return func() tea.Msg {
		res, err := s.GlobalSearch(q, 100)
		return globalSearchMsg{revision: rev, results: res, err: err}
	}
}

// globalSearchKey drives the search modal.
func (m *Model) globalSearchKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.modal = ""
		return nil
	case "enter":
		return m.jumpToResult()
	case "up", "ctrl+k":
		m.choice = max(0, m.choice-1)
		return nil
	case "down", "ctrl+j":
		m.choice = min(max(0, len(m.globalResults)-1), m.choice+1)
		return nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(k)
	return tea.Batch(cmd, m.runGlobalSearch())
}

// jumpToResult opens the result's thread and asks the cache to bring the
// matched message into view.
func (m *Model) jumpToResult() tea.Cmd {
	if m.choice < 0 || m.choice >= len(m.globalResults) {
		return nil
	}
	res := m.globalResults[m.choice]
	m.modal = ""
	var target *domain.Thread
	for i := range m.history.threads {
		if m.history.threads[i].ID == res.ThreadID {
			t := m.history.threads[i]
			target = &t
			break
		}
	}
	if target == nil {
		m.notify("That conversation is not in the cache", true)
		return nil
	}
	m.history.jump = &res
	m.setPane(paneConversation)
	return m.openThread(*target)
}

// globalSearchLabels renders each result as name, date and a body snippet.
func (m *Model) globalSearchLabels() []string {
	if strings.TrimSpace(m.searchInput.Value()) == "" {
		return []string{"Type to search every cached message"}
	}
	if len(m.globalResults) == 0 {
		return []string{"No matches"}
	}
	out := make([]string, 0, len(m.globalResults))
	for _, r := range m.globalResults {
		name := strings.TrimSpace(r.ThreadName)
		if name == "" {
			name = m.nameFor(r.Sender)
		}
		out = append(out, fmt.Sprintf("%s · %s · %s", name, r.Timestamp.Local().Format("Jan 2, 2006"), snippet(r.Body)))
	}
	return out
}

// searchAction handles the palette entry.
func (m *Model) searchAction(name string) (tea.Cmd, bool) {
	if name == "Search all messages" {
		m.openGlobalSearch()
		return nil, true
	}
	return nil, false
}
