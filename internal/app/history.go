package app

import (
	"context"
	"fmt"
	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/storage"
	"github.com/allisonhere/tidesms/internal/syncer"
	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/allisonhere/tidesms/ui/conversation"
	"github.com/allisonhere/tideui"
	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"time"
)

type historyStore interface {
	syncer.Cache
	Threads(string) ([]domain.Thread, error)
	Messages(string, int) ([]domain.Message, error)
	Search(string, string) ([]domain.Message, error)
	MarkRead(string, []string) error
	MarkUnread(string) error
	ThreadTheme(string, string) error
	ThreadBubbleThemes(string, string, string) error
	MessageStatus(string, domain.Status) error
}

const (
	paneContacts = iota
	paneThreads
	paneConversation
	paneComposer
)

type historyState struct {
	wantsRead      bool
	enabled        bool
	store          historyStore
	source         backend.ConversationBackend
	engine         *syncer.Engine
	pane           int
	threads        []domain.Thread
	threadSelected string
	active         *domain.Thread
	view           conversation.Model
	views          map[string]conversation.Model
	limit          int
	cacheSeq       uint64
	cacheApplied   uint64
	sessionDevice  string
	generation     uint64
	cancel         context.CancelFunc
	events         <-chan syncUpdate
	status         string
	older          bool
	search         bool
	searchQuery    string
	searchRevision uint64
	searchResults  []domain.Message
	searchIndex    int
	retryID        string
	started        time.Time
}
type syncUpdate struct {
	status   string
	err      error
	incoming []domain.Message
}
type syncMsg struct {
	generation uint64
	update     syncUpdate
	closed     bool
}
type cacheMsg struct {
	seq            uint64
	device, thread string
	threads        []domain.Thread
	messages       []domain.Message
	err            error
}
type historySavedMsg struct{ err error }
type searchMsg struct {
	revision uint64
	thread   string
	messages []domain.Message
	err      error
}
type outgoingMsg struct {
	message  domain.Message
	draftKey string
	draft    storage.Draft
	err      error
	complete bool
}
type olderMsg struct {
	thread string
	err    error
}

func (m *Model) initHistory() {
	s, ok := m.store.(historyStore)
	b, yes := m.backend.(backend.ConversationBackend)
	if !ok || !yes {
		return
	}
	m.history = historyState{enabled: true, store: s, source: b, engine: &syncer.Engine{Store: s, Backend: b, PageSize: m.cfg.Sync.PageSize}, pane: paneThreads, view: conversation.New(), views: map[string]conversation.Model{}, limit: m.cfg.Sync.InitialMessages, status: "Cached", started: time.Now()}
}
func (m *Model) loadCache() tea.Cmd {
	if !m.history.enabled {
		return nil
	}
	m.history.cacheSeq++
	seq := m.history.cacheSeq
	device := m.deviceID
	thread := ""
	if m.history.active != nil {
		thread = m.history.active.ID
	}
	limit := m.history.limit
	s := m.history.store
	return func() tea.Msg {
		ts, err := s.Threads(device)
		var ms []domain.Message
		if err == nil && thread != "" {
			ms, err = s.Messages(thread, limit)
		}
		return cacheMsg{seq, device, thread, ts, ms, err}
	}
}
func (m *Model) setPane(p int) {
	m.history.pane = p
	m.focus = p == paneComposer
	m.editor.Focus(m.focus && !m.sending)
}
func (m *Model) cyclePane(back bool) {
	if !m.history.enabled {
		m.setFocus(!m.focus)
		return
	}
	// Contacts occupy the threads slot when shown, so Tab moves on from them
	// rather than cycling through a pane that is usually hidden.
	panes := []int{paneThreads, paneConversation, paneComposer}
	if m.width < 70 {
		panes = []int{paneContacts, paneThreads, paneConversation, paneComposer}
	}
	idx := 0
	for i, p := range panes {
		if p == m.history.pane {
			idx = i
		}
	}
	step := 1
	if back {
		step = len(panes) - 1
	}
	m.setPane(panes[(idx+step)%len(panes)])
}
func (m *Model) draftKey() string {
	if m.history.enabled && m.history.active != nil {
		return m.history.active.ID
	}
	return m.recipient.PhoneNumber
}
func (m *Model) openThread(t domain.Thread) tea.Cmd {
	if m.history.active != nil {
		m.history.views[m.history.active.ID] = m.history.view
	}
	m.history.active = &t
	m.history.wantsRead = true
	m.history.threadSelected = t.ID
	m.history.limit = m.cfg.Sync.InitialMessages
	m.history.searchQuery = ""
	m.history.searchResults = nil
	m.history.searchRevision++
	m.history.view = conversation.New()
	if v, ok := m.history.views[t.ID]; ok {
		m.history.view = v
		m.history.limit = max(m.history.limit, len(v.Messages))
	}
	c := contacts.Contact{Name: t.DisplayName}
	if len(t.Participants) == 1 {
		c.PhoneNumber = t.Participants[0].Number
		for _, contact := range m.contacts {
			if contact.PhoneNumber == c.PhoneNumber {
				c = contact
				break
			}
		}
	}
	// Promote a legacy number draft only for an unambiguous single-person thread.
	if _, ok := m.drafts[t.ID]; !ok && len(t.Participants) == 1 {
		if legacy, ok := m.drafts[c.PhoneNumber]; ok && legacy.Body != "" {
			m.drafts[t.ID] = legacy
		}
	}
	m.editorEpoch++
	m.recipient = c
	m.editor.SetValue(m.drafts[t.ID].Body)
	m.setPane(paneConversation)
	m.sizeEditor()
	return tea.Batch(m.loadCache(), m.requestOlder(false))
}
func (m *Model) selectedThread() *domain.Thread {
	for i := range m.history.threads {
		if m.history.threads[i].ID == m.history.threadSelected {
			t := m.history.threads[i]
			return &t
		}
	}
	return nil
}
func (m *Model) layoutConversation() {
	if !m.history.enabled {
		return
	}
	_, right, body, eh := m.dimensions()
	// Participant names are already resolved against local and imported
	// contacts, so a message shows who sent it rather than their number.
	names := map[string]string{}
	if t := m.history.active; t != nil {
		for _, p := range t.Participants {
			if p.Name == "" || p.Name == p.Number {
				continue
			}
			names[p.Number] = p.Name
			if key := contacts.MatchKey(p.Number); key != "" {
				names[key] = p.Name
			}
		}
	}
	conv := m.conversationTheme()
	m.history.view.Layout(tideui.NewRenderer(conv, styleOptions), max(1, right-2), max(1, body-eh-6-noticeLines(m)), conversation.Options{Dates: m.cfg.Conversation.ShowDateSeparators, MaxWidth: m.cfg.Conversation.MaxWidth, Bubbles: m.cfg.Conversation.Bubbles, Corners: m.cfg.Conversation.Corners, Fill: m.cfg.Conversation.FillBubbles, Incoming: m.bubblePalette(conv, false), Outgoing: m.bubblePalette(conv, true), Names: names, Timestamps: m.cfg.Conversation.Timestamps, Query: m.history.searchQuery})
}

// composerNotice says why sending is unavailable, and is absent otherwise. The
// editing mode is already in the status bar, so it is not repeated above the
// composer; the line it used is given back to the conversation.
func (m *Model) composerNotice() string {
	if t := m.history.active; t != nil && t.IsGroup {
		return "Read-only group · sending unavailable"
	}
	return ""
}
func noticeLines(m *Model) int {
	if m.composerNotice() == "" {
		return 0
	}
	return 1
}

var styleOptions = tideui.StyleOptions{PaneCorners: tideui.RoundCorners, ModalShadow: true}

// renderer draws the shell: the lists, the status bar and every modal. It uses
// the global theme alone, so moving between threads never repaints the
// interface around them.
// previewing reports the theme highlighted in an open picker, so moving through
// the list shows the palette rather than only its name.
func (m *Model) previewing(kinds ...string) (string, bool) {
	if m.choice < 0 || m.choice >= len(m.choices) {
		return "", false
	}
	for _, kind := range kinds {
		if m.modal == kind {
			name := m.choices[m.choice]
			if name == "automatic" || name == "inherit" {
				return "", true
			}
			return name, true
		}
	}
	return "", false
}

func (m *Model) renderer() tideui.Renderer {
	name := m.cfg.General.Theme
	if preview, ok := m.settingsThemePreview(); ok && preview != "" {
		name = preview
	}
	return tideui.NewRenderer(themes.Base(name), styleOptions)
}

// conversationTheme resolves the palette for the open conversation: a thread's
// explicit theme wins, then the contact's, then the global one, with each picker
// preview folded in. A conversation keeps its own colours without repainting the
// shell around it.
func (m *Model) conversationTheme() tideui.Theme {
	identity := m.recipient.PhoneNumber
	override := m.recipient.Theme
	if t := m.history.active; t != nil {
		if t.IsGroup {
			identity = t.ID
			override = ""
		}
		if t.ThemeID != "" {
			override = t.ThemeID
		}
	}
	global := m.cfg.General.Theme
	if preview, ok := m.settingsThemePreview(); ok && preview != "" {
		global = preview
	}
	if preview, ok := m.previewing("themes", "thread-themes"); ok {
		override = preview
	}
	if preview, ok := m.settingsContactPreview(); ok {
		override = preview
	}
	return themes.Resolve(global, override, identity)
}

func (m *Model) conversationRenderer() tideui.Renderer {
	return tideui.NewRenderer(m.conversationTheme(), styleOptions)
}

// bubblePalette resolves a direction's message surface: a thread override beats
// a contact override, which beats the global default, and an open picker
// previews on top.
func (m *Model) bubblePalette(conv tideui.Theme, outgoing bool) themes.Bubble {
	name := ""
	if t := m.history.active; t != nil {
		if outgoing {
			name = t.ThemeOut
		} else {
			name = t.ThemeIn
		}
	}
	if name == "" {
		if outgoing {
			name = m.recipient.ThemeOut
		} else {
			name = m.recipient.ThemeIn
		}
	}
	if name == "" {
		if outgoing {
			name = m.cfg.Conversation.OutgoingTheme
		} else {
			name = m.cfg.Conversation.IncomingTheme
		}
	}
	if preview, ok := m.settingsBubblePreview(outgoing); ok {
		name = preview
	}
	if preview, ok := m.bubblePickerPreview(outgoing); ok {
		name = preview
	}
	return themes.BubbleFor(conv, name, outgoing)
}

// bubblePickerPreview reports the theme highlighted in the open scoped bubble
// picker for one direction.
func (m *Model) bubblePickerPreview(outgoing bool) (string, bool) {
	if m.modal != "bubble-themes" {
		return "", false
	}
	if (m.bubbleDir == "out") != outgoing {
		return "", false
	}
	if m.choice < 0 || m.choice >= len(m.choices) {
		return "", false
	}
	name := m.choices[m.choice]
	if name == "automatic" || name == "inherit" {
		return "", true
	}
	return name, true
}
func (m *Model) startSession(force bool) tea.Cmd {
	h := &m.history
	if !h.enabled || m.deviceID == "" {
		return nil
	}
	if h.sessionDevice == m.deviceID && !force {
		return nil
	}
	if h.cancel != nil {
		h.cancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	h.cancel = cancel
	h.generation++
	generation := h.generation
	device := m.deviceID
	h.sessionDevice = device
	h.status = "Connecting…"
	ch := make(chan syncUpdate, 128)
	h.events = ch
	engine := h.engine
	source := h.source
	store := h.store
	go func() {
		defer close(ch)
		emit := func(v syncUpdate) bool {
			select {
			case ch <- v:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for ctx.Err() == nil {
			sessionCtx, stop := context.WithCancel(ctx)
			stream, err := source.Subscribe(sessionCtx, device)
			if err != nil {
				stop()
				if !emit(syncUpdate{status: "Offline · reconnecting", err: err}) {
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(3 * time.Second):
					continue
				}
			}
			done := make(chan error, 1)
			go func() {
				done <- engine.Refresh(sessionCtx, device, func(n int) { emit(syncUpdate{status: fmt.Sprintf("Syncing %d threads…", n)}) })
			}()
			emit(syncUpdate{status: "Syncing…"})
		session:
			for {
				select {
				case <-ctx.Done():
					stop()
					if done != nil {
						<-done
					}
					return
				case err := <-done:
					done = nil
					if err != nil {
						emit(syncUpdate{status: "Sync incomplete · cached", err: err})
					} else {
						emit(syncUpdate{status: "Synced " + time.Now().Format("15:04")})
					}
				case event, ok := <-stream:
					if !ok || event.Kind == domain.EventConnection || event.Kind == domain.EventRefresh {
						stop()
						if done != nil {
							<-done
						}
						break session
					}
					if event.Err != nil {
						emit(syncUpdate{err: event.Err})
						continue
					}
					if event.Message != nil {
						added, err := store.MergeMessages([]domain.Message{*event.Message})
						if !emit(syncUpdate{incoming: added, err: err}) {
							stop()
							if done != nil {
								<-done
							}
							return
						}
					}
				}
			}
			stop()
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()
	return waitSync(ch, generation)
}
func waitSync(ch <-chan syncUpdate, generation uint64) tea.Cmd {
	return func() tea.Msg { v, ok := <-ch; return syncMsg{generation, v, !ok} }
}
func (m *Model) requestOlder(expand bool) tea.Cmd {
	h := &m.history
	if h.active == nil || h.older {
		return nil
	}
	t := *h.active
	if t.BackendID == "" || strings.HasPrefix(t.BackendID, "local-") {
		return m.loadCache()
	}
	if expand {
		h.limit += m.cfg.Sync.PageSize
	}
	h.older = true
	offset := max(0, h.limit-m.cfg.Sync.PageSize)
	limit := m.cfg.Sync.PageSize
	engine := h.engine
	ctx := m.ctx
	return func() tea.Msg { err := engine.Older(ctx, t, offset, limit); return olderMsg{t.ID, err} }
}
func (m *Model) markVisibleRead() tea.Cmd {
	h := &m.history
	if h.active == nil || !h.view.AtNewest() || (h.pane != paneConversation && h.pane != paneComposer) || m.modal != "" {
		return nil
	}
	ids := []string{}
	for _, v := range h.view.Messages {
		if v.Unread {
			ids = append(ids, v.ID)
		}
	}
	if len(ids) == 0 && h.active.UnreadCount == 0 {
		return nil
	}
	thread := h.active.ID
	s := h.store
	return func() tea.Msg { return historySavedMsg{s.MarkRead(thread, ids)} }
}
func (m *Model) searchThread() tea.Cmd {
	h := &m.history
	h.searchRevision++
	rev := h.searchRevision
	if h.active == nil {
		return nil
	}
	thread := h.active.ID
	query := h.searchQuery
	s := h.store
	return func() tea.Msg { ms, err := s.Search(thread, query); return searchMsg{rev, thread, ms, err} }
}
func (m *Model) copyText(text string) tea.Cmd {
	return func() tea.Msg { return historySavedMsg{clipboard.WriteAll(text)} }
}
func (m *Model) conversationKey(k tea.KeyMsg) tea.Cmd {
	h := &m.history
	if h.search {
		switch k.String() {
		case "esc":
			h.search = false
			h.searchQuery = ""
			h.searchResults = nil
			h.searchRevision++
			m.layoutConversation()
			return m.loadCache()
		case "enter":
			h.search = false
			return m.searchThread()
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(k)
		h.searchQuery = m.filter.Value()
		return tea.Batch(cmd, m.searchThread())
	}
	if h.pane == paneThreads {
		idx := 0
		for i, t := range h.threads {
			if t.ID == h.threadSelected {
				idx = i
			}
		}
		switch k.String() {
		case "j", "down":
			idx = min(len(h.threads)-1, idx+1)
		case "k", "up":
			idx = max(0, idx-1)
		case "G", "end":
			idx = len(h.threads) - 1
		case "g", "home":
			idx = 0
		case "enter":
			if t := m.selectedThread(); t != nil {
				return m.openThread(*t)
			}
		case "r":
			return m.startSession(true)
		case "n":
			return m.openCompose()
		case "?":
			m.modal = "help"
		case "q", "ctrl+c":
			return m.quit()
		case "c":
			m.setPane(paneContacts)
		}
		if idx >= 0 && idx < len(h.threads) {
			h.threadSelected = h.threads[idx].ID
		}
		return nil
	}
	switch k.String() {
	case "j", "down":
		h.view.Move(1)
	case "k", "up":
		h.view.Move(-1)
	case "pgup", "ctrl+u":
		h.view.Scroll(-max(1, h.view.Height-2))
	case "pgdown", "ctrl+d":
		h.view.Scroll(max(1, h.view.Height-2))
	case "g", "home":
		h.view.Oldest()
	case "G", "end":
		h.view.Newest()
	case "enter":
		m.modal = "message"
		m.choice = 0
		m.choices = []string{"Copy", "Reply", "Close"}
		if msg := h.view.Current(); msg != nil && msg.Status == domain.Failed && msg.BackendID == "" {
			m.choices = []string{"Copy", "Reply", "Retry", "Close"}
		}
	case "y":
		if msg := h.view.Current(); msg != nil {
			return m.copyText(msg.Body)
		}
	case "r":
		if msg := h.view.Current(); msg != nil && msg.Status == domain.Failed && msg.BackendID == "" {
			h.retryID = msg.ID
			m.editor.SetValue(msg.Body)
			m.trackChange()
		}
		m.setPane(paneComposer)
	case "/":
		if h.active != nil {
			h.search = true
			m.filter.SetValue("")
			h.searchQuery = ""
			return m.filter.Focus()
		}
	case "n", "N":
		if len(h.searchResults) > 0 {
			delta := 1
			if k.String() == "N" {
				delta = len(h.searchResults) - 1
			}
			h.searchIndex = (h.searchIndex + delta) % len(h.searchResults)
			h.view.Selected = h.searchIndex
			h.view.Follow = false
			m.layoutConversation()
		}
	case "esc":
		h.searchQuery = ""
		h.searchResults = nil
		h.searchRevision++
		return m.loadCache()
	case "q", "ctrl+c":
		return m.quit()
	case "?":
		m.modal = "help"
	}
	if h.view.Selected < 3 && h.searchQuery == "" && (k.String() == "k" || k.String() == "up" || k.String() == "pgup" || k.String() == "g") {
		return tea.Batch(m.loadCache(), m.requestOlder(true))
	}
	return m.markVisibleRead()
}
func (m *Model) sendHistory() tea.Cmd {
	if !m.prepareSend() {
		return nil
	}
	return m.deliverPending()
}

func (m *Model) historyUpdate(raw tea.Msg) (bool, tea.Cmd) {
	h := &m.history
	if !h.enabled {
		return false, nil
	}
	switch v := raw.(type) {
	case cacheMsg:
		if v.device != m.deviceID || v.seq < h.cacheApplied {
			return true, nil
		}
		h.cacheApplied = v.seq
		if v.err != nil {
			m.notify("Could not read conversation cache", true)
			m.logError("cache", v.err)
			return true, nil
		}
		// Keep selection by identity even when activity changes sort order.
		if h.pane == paneThreads && len(h.threads) > 0 {
			stable := make([]domain.Thread, 0, len(v.threads))
			seen := map[string]bool{}
			byID := map[string]domain.Thread{}
			for _, t := range v.threads {
				byID[t.ID] = t
			}
			for _, old := range h.threads {
				if t, ok := byID[old.ID]; ok {
					stable = append(stable, t)
					seen[t.ID] = true
				}
			}
			for _, t := range v.threads {
				if !seen[t.ID] {
					stable = append(stable, t)
				}
			}
			h.threads = stable
		} else {
			h.threads = v.threads
		}
		if m.selectedThread() == nil && len(h.threads) > 0 {
			h.threadSelected = h.threads[0].ID
		}

		if h.active != nil && strings.Contains(h.active.ID, ":local-") {
			for _, t := range h.threads {
				if t.ID != h.active.ID && !t.IsGroup && len(t.Participants) == 1 && t.Participants[0].Number == m.recipient.PhoneNumber {
					old := h.active.ID
					copy := t
					h.active = &copy
					h.threadSelected = t.ID
					m.drafts[t.ID] = m.drafts[old]
					return true, m.loadCache()
				}
			}
		}
		if h.active != nil {
			for _, t := range h.threads {
				if t.ID == h.active.ID {
					copy := t
					h.active = &copy
					break
				}
			}
		}
		if h.active != nil && v.thread == h.active.ID && h.searchQuery == "" {
			h.view.SetMessages(v.messages)
			m.layoutConversation()
			if h.wantsRead {
				h.wantsRead = false
				return true, m.markVisibleRead()
			}
		}
		return true, nil
	case syncMsg:
		if v.generation != h.generation {
			return true, nil
		}
		if v.closed {
			h.sessionDevice = ""
			h.status = "Offline"
			return true, nil
		}
		if v.update.status != "" {
			h.status = v.update.status
		}
		if v.update.err != nil {
			m.logError("conversation sync", v.update.err)
			m.notify(v.update.err.Error(), true)
		}
		cmds := []tea.Cmd{waitSync(h.events, h.generation), m.loadCache()}
		for _, msg := range v.update.incoming {
			visible := h.active != nil && h.active.ID == msg.ThreadID && (h.pane == paneConversation || h.pane == paneComposer) && h.view.AtNewest() && m.modal == ""
			if visible {
				thread, id := msg.ThreadID, msg.ID
				s := h.store
				cmds = append(cmds, func() tea.Msg { return historySavedMsg{s.MarkRead(thread, []string{id})} })
			}
			if !visible && m.cfg.Notifications.Enabled && msg.Direction == domain.Incoming && msg.Timestamp.After(h.started) {
				name := msg.Sender
				for _, c := range m.contacts {
					if c.PhoneNumber == msg.Sender {
						name = c.Name
					}
				}
				body := msg.Body
				if !m.cfg.Notifications.ShowBody {
					body = "New SMS"
				}
				ctx := m.ctx
				notify := m.notifier
				cmds = append(cmds, func() tea.Msg {
					if err := notify(ctx, name, body); err != nil {
						m.logError("desktop notification", err)
					}
					return nil
				})
			}
		}
		return true, tea.Batch(cmds...)
	case historySavedMsg:
		if v.err != nil {
			m.notify("Could not update conversation state", true)
			m.logError("conversation action", v.err)
		}
		return true, m.loadCache()
	case searchMsg:
		if v.revision != h.searchRevision || h.active == nil || v.thread != h.active.ID {
			return true, nil
		}
		if v.err != nil {
			m.notify("Could not search cached messages", true)
			return true, nil
		}
		h.searchResults = v.messages
		h.searchIndex = 0
		h.view.Follow = false
		h.view.SetMessages(v.messages)
		h.view.Selected = 0
		m.layoutConversation()
		return true, nil
	case olderMsg:
		h.older = false
		if v.err != nil {
			m.notify("Older history unavailable; cached messages remain usable", true)
			m.logError("older history", v.err)
		}
		return true, m.loadCache()
	case outgoingMsg:
		if !v.complete && v.err == nil {
			phone := ""
			if len(v.message.Participants) == 1 {
				phone = v.message.Participants[0].Number
			}
			b := m.backend
			s := h.store
			ctx := m.ctx
			msg := v.message
			return true, tea.Batch(m.loadCache(), func() tea.Msg {
				err := b.Send(ctx, backend.SendRequest{DeviceID: msg.DeviceID, PhoneNumber: phone, ThreadID: msg.ThreadID, Message: msg.Body})
				status := domain.Submitted
				if err != nil {
					status = domain.Failed
				}
				if saveErr := s.MessageStatus(msg.ID, status); saveErr != nil && err == nil {
					err = fmt.Errorf("submission accepted but its local status could not be saved; do not retry automatically")
				}
				v.err = err
				v.complete = true
				return v
			})
		}
		m.sending = false
		m.editor.Focus(m.focus)
		if v.err != nil {
			m.notify(v.err.Error(), true)
			return true, m.loadCache()
		}
		m.notify("✓ Submitted · delivery unverified", false)
		d := m.drafts[v.draftKey]
		if d.Revision == v.draft.Revision && d.Body == v.draft.Body {
			d.Body = ""
			d.Revision++
			m.drafts[v.draftKey] = d
			if m.draftKey() == v.draftKey {
				m.editor.SetValue("")
			}
			return true, tea.Batch(m.saveDraft(v.draftKey, d), m.loadCache())
		}
		return true, m.loadCache()
	}
	return false, nil
}
func (m *Model) historyAction(name string) (bool, tea.Cmd) {
	h := &m.history
	if !h.enabled {
		return false, nil
	}
	switch name {
	case "Refresh conversations":
		m.modal = ""
		return true, m.startSession(true)
	case "Search current thread":
		m.modal = ""
		m.setPane(paneConversation)
		return true, m.conversationKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	case "Jump to newest":
		m.modal = ""
		m.setPane(paneConversation)
		h.searchQuery = ""
		h.view.Newest()
		return true, tea.Batch(m.loadCache(), m.markVisibleRead())
	case "Change thread theme":
		if h.active == nil {
			m.notify("Open a thread first", true)
			return true, nil
		}
		m.modal = "thread-themes"
		m.choice = 0
		m.choices = append([]string{"inherit"}, themes.Names...)
		return true, nil
	case "Change thread incoming bubble theme":
		if h.active == nil {
			m.notify("Open a thread first", true)
			return true, nil
		}
		m.openBubblePicker("thread", false)
		return true, nil
	case "Change thread outgoing bubble theme":
		if h.active == nil {
			m.notify("Open a thread first", true)
			return true, nil
		}
		m.openBubblePicker("thread", true)
		return true, nil
	case "Change thread AI policy":
		if h.active == nil {
			m.notify("Open a thread first", true)
			return true, nil
		}
		m.openAIPolicyPicker(storage.ScopeThread)
		return true, nil
	case "Mark thread unread":
		if h.active == nil {
			return true, nil
		}
		thread := h.active.ID
		s := h.store
		m.modal = ""
		return true, func() tea.Msg { return historySavedMsg{s.MarkUnread(thread)} }
	case "Copy phone number":
		m.modal = ""
		if h.active != nil {
			numbers := []string{}
			for _, p := range h.active.Participants {
				numbers = append(numbers, p.RawNumber)
			}
			return true, m.copyText(strings.Join(numbers, ", "))
		}
		return true, m.copyText(m.recipient.PhoneNumber)
	case "Open contact":
		m.modal = ""
		m.setPane(paneContacts)
		for i, c := range m.filtered() {
			if c.PhoneNumber == m.recipient.PhoneNumber {
				m.selected = i
				break
			}
		}
		return true, nil
	}
	return false, nil
}
