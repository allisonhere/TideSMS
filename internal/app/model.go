package app

import (
	"context"
	"fmt"
	"github.com/allisonhere/tidesms/internal/ai"
	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/config"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/keys"
	"github.com/allisonhere/tidesms/internal/media"
	"github.com/allisonhere/tidesms/internal/notifications"
	"github.com/allisonhere/tidesms/internal/search"
	"github.com/allisonhere/tidesms/internal/storage"
	"github.com/allisonhere/tidesms/ui/composer"
	"github.com/allisonhere/tidesms/ui/conversation"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"log/slog"
	"sort"
	"strings"
	"time"
)

type Repository interface {
	Contacts() ([]contacts.Contact, error)
	SaveContact(contacts.Contact) error
	DeleteContact(string) error
	Drafts() (map[string]storage.Draft, error)
	SaveDraft(string, storage.Draft) error
	SyncedContacts(string) ([]contacts.Synced, error)
	ReplaceSyncedContacts(string, []contacts.Synced) error
	// AIPolicy returns a stored per-contact or per-thread privacy override.
	AIPolicy(scope, id string) (policy, provider, model string, ok bool, err error)
	SetAIPolicy(scope, id, policy, provider, model string) error
	ClearAIPolicy(scope, id string) error
	// NotificationMode returns a stored per-contact or per-thread override.
	NotificationMode(scope, id string) (mode string, ok bool, err error)
	SetNotificationMode(scope, id, mode string) error
	ClearNotificationMode(scope, id string) error
	DeleteMessage(string) error
}
type Model struct {
	history       historyState
	editorEpoch   uint64
	store         Repository
	backend       backend.MessagingBackend
	log           *slog.Logger
	cfg           config.Config
	configPath    string
	configLocked  bool
	ctx           context.Context
	width, height int
	// graphics is the terminal's image protocol and cellW/cellH its cell size
	// in pixels, both re-read on resize: a window moved to a display with a
	// different scale changes the cell size, and an image sized to the old one
	// would no longer keep its proportions.
	graphics                                        media.Protocol
	cellW, cellH                                    int
	ready, loaded, focus, searching, busy, quitting bool
	contacts                                        []contacts.Contact
	synced                                          []contacts.Synced
	contactsDevice                                  string
	selected                                        int
	query                                           string
	recipient                                       contacts.Contact
	editor                                          composer.Model
	drafts                                          map[string]storage.Draft
	devices                                         []backend.Device
	deviceID                                        string
	discovering                                     bool
	promptedDevices                                 bool
	notice                                          string
	failed                                          bool
	modal                                           string
	choices                                         []string
	picks                                           []contacts.Contact
	choice                                          int
	filter                                          textinput.Model
	fields                                          []textinput.Model
	field                                           int
	editing                                         contacts.Contact
	// themeCursor, contactCursor and the bubble cursors hold the value
	// highlighted in the static settings panel before it is committed, so the
	// theme previews live.
	themeCursor         int
	contactCursor       int
	aiProviderCursor    int
	aiPolicyCursor      int
	localProviderStatus map[ai.Provider]localProviderStatus
	lookupAfterKey      bool
	// settingEdit is the settings panel's inline text editor, used by the rows
	// that are typed rather than cycled.
	settingEdit            bool
	settingEditing         settingID
	settingInput           textinput.Model
	bubbleInCursor         int
	bubbleOutCursor        int
	bubbleScope, bubbleDir string
	policyScope            string
	sending                bool
	sendPhone              string
	// assistant is the configured AI writer, or the null assistant when AI is
	// off. The draft is never sent under a policy the provider does not satisfy.
	assistant ai.WritingAssistant
	aiCancel  context.CancelFunc
	aiBusy    bool
	aiEpoch   uint64
	review    aiReviewState
	aiInput   textinput.Model
	// Global message search.
	searchInput   textinput.Model
	globalResults []search.Result
	searchRev     uint64
	// deleteMsgID is the message awaiting Delete-local-copy confirmation.
	deleteMsgID string
	// Media viewer: the attachments of one message, the highlighted part, and
	// whether the terminal can draw images inline.
	mediaAtts       []domain.Attachment
	mediaIndex      int
	mediaMsgID      string
	pendingOpenPath string
	// previewAfterFetch is the attachment id v is waiting on, so the viewer
	// opens by itself once the real file has been downloaded.
	previewAfterFetch string
	// settingsRow is the panel's cursor while a picker opened from it borrows
	// m.choice, so leaving the picker returns to the row it was opened from.
	settingsRow int
	// modelListFailed is the configuration a model listing last failed for, so
	// the same doomed request is not made again on every Enter.
	modelListFailed string
	// pending is a composed message awaiting send, queue or schedule.
	pending        *pendingSend
	outboxEntries  []outboxEntry
	schedInput     textinput.Model
	queuedCount    int
	scheduledCount int
	// notifier is replaced by tests; production always uses the desktop service.
	notifier func(context.Context, string, string) error
}
type editorMsg struct {
	epoch uint64
	msg   tea.Msg
}

type loadedMsg struct {
	contacts []contacts.Contact
	synced   []contacts.Synced
	drafts   map[string]storage.Draft
	err      error
}
type contactsMsg struct {
	synced []contacts.Synced
	manual bool
	err    error
}
type devicesMsg struct {
	devices []backend.Device
	err     error
}
type refreshMsg struct{}
type debounceMsg struct {
	phone    string
	revision int64
}
type savedMsg struct{ err error }
type mutationMsg struct {
	contact *contacts.Contact
	deleted string
	err     error
}
type configMsg struct {
	cfg config.Config
	err error
}
type sentMsg struct {
	phone, name, body string
	revision          int64
	err               error
}
type quitMsg struct{ err error }

func New(ctx context.Context, s Repository, b backend.MessagingBackend, c config.Config, path string, log *slog.Logger, startupError error) *Model {
	m := &Model{ctx: ctx, store: s, backend: b, cfg: c, configPath: path, log: log, editor: composer.New(c.Composer.Mode), drafts: map[string]storage.Draft{}, deviceID: c.KDEConnect.PreferredDevice, notifier: notifications.Show}
	m.filter = textinput.New()
	m.filter.CharLimit = 100
	m.aiInput = textinput.New()
	m.aiInput.CharLimit = 500
	m.aiInput.Placeholder = "How should it be rewritten?"
	m.schedInput = textinput.New()
	m.schedInput.CharLimit = 32
	m.schedInput.Placeholder = "YYYY-MM-DD HH:MM"
	m.searchInput = textinput.New()
	m.searchInput.CharLimit = 120
	m.searchInput.Placeholder = "Search all messages…"
	m.assistant = buildAssistant(c)
	if startupError != nil {
		m.configLocked = true
		m.notify(startupError.Error(), true)
	}
	m.initHistory()
	return m
}

// buildAssistant turns the resolved AI config into a provider. Disabled config
// yields the null assistant, which refuses rather than calling anywhere.
func buildAssistant(c config.Config) ai.WritingAssistant {
	if !c.AI.Enabled {
		return ai.Disabled{}
	}
	return ai.New(ai.Runtime{Provider: ai.Provider(c.AI.Provider), Endpoint: c.AI.Endpoint, Model: c.AI.Model, APIKey: c.AI.APIKey})
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(func() tea.Msg {
		if m.store == nil {
			return loadedMsg{err: fmt.Errorf("SQLite is unavailable; restart after fixing the state directory")}
		}
		cs, err := m.store.Contacts()
		if err != nil {
			return loadedMsg{err: err}
		}
		sy, err := m.store.SyncedContacts(m.deviceID)
		if err != nil {
			return loadedMsg{err: err}
		}
		ds, err := m.store.Drafts()
		return loadedMsg{cs, sy, ds, err}
	}, m.discover(), m.loadCache(), m.processQueue(), m.refreshCounts())
}
func (m *Model) notify(s string, failed bool) { m.notice = s; m.failed = failed }
func (m *Model) discover() tea.Cmd {
	m.discovering = true
	return func() tea.Msg { ds, err := m.backend.Devices(m.ctx); return devicesMsg{ds, err} }
}
func (m *Model) currentDevice() *backend.Device {
	for i := range m.devices {
		if m.devices[i].ID == m.deviceID {
			return &m.devices[i]
		}
	}
	if m.deviceID != "" {
		return &backend.Device{ID: m.deviceID, Name: "Preferred phone"}
	}
	return nil
}

// allContacts lists the user's own contacts first, then the phone's address
// book. A synced entry whose number is already a local contact is hidden, so the
// local name and accent always win. Numbers are compared tolerantly, because a
// phone commonly stores the same person as 8165550182 and +18165550182.
func (m *Model) allContacts() []contacts.Contact {
	known := map[string]bool{}
	out := make([]contacts.Contact, 0, len(m.contacts)+len(m.synced))
	for _, c := range m.contacts {
		known[c.PhoneNumber] = true
		if key := contacts.MatchKey(c.PhoneNumber); key != "" {
			known[key] = true
		}
		out = append(out, c)
	}
	for _, s := range m.synced {
		if known[s.PhoneNumber] || known[contacts.MatchKey(s.PhoneNumber)] {
			continue
		}
		c := contacts.Contact{Name: s.Name, PhoneNumber: s.PhoneNumber, Synced: true}
		if c.Name == "" {
			c.Name = s.RawNumber
		}
		out = append(out, c)
	}
	return out
}

// conversing reports the numbers that already have a thread, matched tolerantly
// so a contact stored without a country code still counts.
func (m *Model) conversing() map[string]bool {
	out := map[string]bool{}
	for _, t := range m.history.threads {
		for _, p := range t.Participants {
			out[p.Number] = true
			if key := contacts.MatchKey(p.Number); key != "" {
				out[key] = true
			}
		}
	}
	return out
}

// filtered is the contact list the sidebar shows. An imported address book is
// mostly people you have never texted, so synced entries appear only once a
// conversation exists; everything remains reachable through the new-message
// search. Contacts the user created are always listed.
func (m *Model) filtered() []contacts.Contact {
	query := strings.ToLower(m.query)
	talking := m.conversing()
	var out []contacts.Contact
	for _, c := range m.allContacts() {
		if c.Synced && !talking[c.PhoneNumber] && !talking[contacts.MatchKey(c.PhoneNumber)] {
			continue
		}
		if !strings.Contains(strings.ToLower(c.Name+" "+c.PhoneNumber), query) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// pickRecipient opens the existing conversation with someone when there is one,
// so starting a message never creates a second thread for the same person.
func (m *Model) pickRecipient(c contacts.Contact) tea.Cmd {
	m.modal = ""
	key := contacts.MatchKey(c.PhoneNumber)
	for _, t := range m.history.threads {
		if t.IsGroup || len(t.Participants) != 1 {
			continue
		}
		p := t.Participants[0]
		if p.Number == c.PhoneNumber || (key != "" && contacts.MatchKey(p.Number) == key) {
			cmd := m.openThread(t)
			m.focusArea(paneComposer)
			return cmd
		}
	}
	m.choose(c)
	return nil
}

// syncContacts imports the phone's address book as a read-only overlay.
func (m *Model) syncContacts(manual bool) tea.Cmd {
	source, ok := m.backend.(backend.ContactsBackend)
	if !ok || m.store == nil {
		if manual {
			m.notify("This backend cannot read phone contacts", true)
		}
		return nil
	}
	if !m.cfg.Contacts.SyncFromPhone {
		if manual {
			m.notify("Contact import is disabled; set sync_from_phone = true in config.toml", true)
		}
		return nil
	}
	if m.deviceID == "" {
		if manual {
			m.notify("Choose a phone first", true)
		}
		return nil
	}
	device, repo, ctx := m.deviceID, m.store, m.ctx
	if manual {
		m.notify("Importing contacts from your phone…", false)
	}
	return func() tea.Msg {
		cs, err := source.SyncContacts(ctx, device)
		if err == nil {
			err = repo.ReplaceSyncedContacts(device, cs)
		}
		if err != nil {
			return contactsMsg{manual: manual, err: err}
		}
		all, err := repo.SyncedContacts(device)
		return contactsMsg{synced: all, manual: manual, err: err}
	}
}

// maybeSyncContacts imports once per phone per run; the plugin refreshes its own
// cache whenever the device connects.
func (m *Model) maybeSyncContacts() tea.Cmd {
	if m.deviceID == "" || m.contactsDevice == m.deviceID {
		return nil
	}
	m.contactsDevice = m.deviceID
	return m.syncContacts(false)
}
func (m *Model) selectedContact() (contacts.Contact, bool) {
	cs := m.filtered()
	if len(cs) == 0 {
		return contacts.Contact{}, false
	}
	m.selected = max(0, min(m.selected, len(cs)-1))
	return cs[m.selected], true
}
func (m *Model) setFocus(on bool) {
	m.focus = on
	m.editor.Focus(on && !m.sending)
	if m.history.enabled {
		if on {
			m.history.pane = paneComposer
		} else {
			m.history.pane = paneContacts
		}
	}
}

// leaveComposer moves focus out of the editor and back to the conversation,
// keeping the draft in place. A message to someone with no thread yet has no
// conversation to return to, so it falls back to the thread list instead.
func (m *Model) leaveComposer() tea.Cmd {
	if m.history.enabled {
		if m.history.active == nil {
			m.setPane(paneThreads)
			return nil
		}
		m.setPane(paneConversation)
		return m.markVisibleRead()
	}
	m.setFocus(false)
	return nil
}

func (m *Model) choose(c contacts.Contact) {
	if m.history.enabled {
		m.history.active = nil
		m.history.view = conversation.New()
	}
	m.editorEpoch++
	m.recipient = c
	m.editor = composer.New(m.cfg.Composer.Mode)
	m.editor.SetValue(m.drafts[c.PhoneNumber].Body)
	m.sizeEditor()
	m.setFocus(true)
}
func (m *Model) trackChange() tea.Cmd {
	phone := m.draftKey()
	if phone == "" {
		return nil
	}
	d := m.drafts[phone]
	if d.Body == m.editor.Value() {
		return nil
	}
	d.Body = m.editor.Value()
	d.Revision++
	m.drafts[phone] = d
	return tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return debounceMsg{phone, d.Revision} })
}
func (m *Model) saveDraft(phone string, d storage.Draft) tea.Cmd {
	return func() tea.Msg { return savedMsg{m.store.SaveDraft(phone, d)} }
}
func (m *Model) saveConfig(c config.Config) tea.Cmd {
	if m.configLocked {
		m.notify("Fix config.toml and restart before saving settings; original file preserved", true)
		return nil
	}
	m.busy = true
	path := m.configPath
	return func() tea.Msg { return configMsg{c, config.Save(path, c)} }
}
func (m *Model) quit() tea.Cmd {
	if m.sending {
		m.notify("Wait for the current send to finish before quitting", true)
		return nil
	}
	if m.busy {
		return nil
	}
	m.quitting = true
	ds := make(map[string]storage.Draft, len(m.drafts))
	for p, d := range m.drafts {
		ds[p] = d
	}
	return func() tea.Msg {
		if m.store != nil {
			for p, d := range ds {
				if err := m.store.SaveDraft(p, d); err != nil {
					return quitMsg{err}
				}
			}
		}
		return quitMsg{}
	}
}
func (m *Model) send() tea.Cmd {
	if m.history.enabled {
		return m.sendHistory()
	}
	if m.sending || !m.loaded {
		return nil
	}
	if m.recipient.PhoneNumber == "" {
		m.notify("Choose a recipient first", true)
		return nil
	}
	if strings.TrimSpace(m.editor.Value()) == "" {
		m.notify("Write a message before sending", true)
		return nil
	}
	d := m.currentDevice()
	if d == nil || !d.Connected {
		m.notify("Could not send — phone disconnected; switch device or reconnect", true)
		return nil
	}
	phone := m.recipient.PhoneNumber
	draft := m.drafts[phone]
	name := m.recipient.Name
	device := d.ID
	m.sending = true
	m.sendPhone = phone
	m.editor.Focus(false)
	m.notify("Sending…", false)
	return func() tea.Msg {
		// Persist before external side effects so a crash cannot discard an unsent draft.
		err := m.store.SaveDraft(phone, draft)
		if err != nil {
			m.logError("save before send", err)
			return sentMsg{phone, name, draft.Body, draft.Revision, fmt.Errorf("could not save draft; message was not submitted")}
		}
		if err == nil {
			err = m.backend.Send(m.ctx, backend.SendRequest{DeviceID: device, PhoneNumber: phone, Message: draft.Body})
		}
		return sentMsg{phone, name, draft.Body, draft.Revision, err}
	}
}
func (m *Model) Update(raw tea.Msg) (tea.Model, tea.Cmd) {
	msg := keys.Normalize(raw)
	if handled, cmd := m.historyUpdate(msg); handled {
		return m, cmd
	}
	switch v := msg.(type) {
	case editorMsg:
		if v.epoch != m.editorEpoch || !m.focus || m.modal != "" || m.sending {
			return m, nil
		}
		if _, ok := v.msg.(composer.CancelMsg); ok {
			return m, m.leaveComposer()
		}
		return m, m.updateEditor(v.msg)
	case aiResultMsg:
		m.handleAIResult(v)
		return m, nil
	case outboxChangedMsg:
		if v.err != nil {
			m.notify("Could not update the outbox", true)
			return m, nil
		}
		if v.draftKey != "" {
			d := m.drafts[v.draftKey]
			if d.Revision == v.draft.Revision && d.Body == v.draft.Body {
				d.Body = ""
				d.Revision++
				m.drafts[v.draftKey] = d
				if m.draftKey() == v.draftKey {
					m.editor.SetValue("")
				}
				return m, tea.Batch(m.saveDraft(v.draftKey, d), m.loadCache(), m.refreshCounts(), m.processQueue())
			}
			// The draft moved on after the message was captured. Queueing it is
			// still what the user asked for; the newer text stays in the editor
			// and we say so rather than silently appearing to duplicate.
			if v.queued {
				m.notify("Queued the captured text; your newer draft is kept", false)
				return m, tea.Batch(m.loadCache(), m.refreshCounts(), m.processQueue())
			}
		}
		if v.scheduled {
			m.notify("Scheduled for "+v.when.Local().Format("Jan 2 3:04 PM"), false)
		} else if v.queued {
			m.notify("Queued for delivery", false)
		}
		return m, tea.Batch(m.loadCache(), m.refreshCounts(), m.processQueue())
	case queueProcessedMsg:
		if v.err != nil && m.ctx.Err() == nil {
			m.notify("Queue could not be processed", true)
		} else if v.failed > 0 {
			m.notify(fmt.Sprintf("%d queued message(s) failed", v.failed), true)
		} else if v.sent > 0 {
			m.notify(fmt.Sprintf("Sent %d queued message(s)", v.sent), false)
		}
		return m, tea.Batch(m.loadCache(), m.refreshCounts())
	case globalSearchMsg:
		if v.revision != m.searchRev {
			return m, nil
		}
		if v.err != nil {
			m.notify("Search failed", true)
			return m, nil
		}
		m.globalResults = v.results
		m.choice = 0
		return m, nil
	case countsMsg:
		m.queuedCount, m.scheduledCount = v.queued, v.scheduled
		return m, nil
	case attachmentOpenedMsg:
		if v.err != nil {
			m.notify("Could not open the file", true)
		}
		return m, nil
	case attachmentFetchedMsg:
		return m, m.applyFetchedAttachment(v)
	case attachmentSavedMsg:
		if v.err != nil {
			m.notify("Could not save the file", true)
			m.logError("save attachment", v.err)
		} else {
			m.notify("Saved to "+v.path, false)
		}
		return m, nil
	case editOutboxMsg:
		return m, tea.Batch(m.prepareEdit(v), m.refreshCounts())
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		m.ready = true
		m.measureTerminal()
		m.sizeEditor()
		return m, nil
	case loadedMsg:
		if v.err != nil {
			m.notify("Could not load SQLite state; check the log and restart", true)
			m.logError("load state", v.err)
			return m, nil
		}
		m.contacts = v.contacts
		m.synced = v.synced
		m.drafts = v.drafts
		m.loaded = true
		return m, m.refreshCounts()
	case contactsMsg:
		if v.err != nil {
			m.logError("contact sync", v.err)
			// A background attempt stays quiet; only an explicit request reports.
			if v.manual {
				m.notify(v.err.Error(), true)
			}
			return m, nil
		}
		m.synced = v.synced
		if v.manual {
			m.notify(fmt.Sprintf("Imported %d phone contacts", len(v.synced)), false)
		}
		return m, m.loadCache()
	case devicesMsg:
		m.discovering = false
		m.devices = v.devices
		if v.err != nil {
			m.notify(v.err.Error(), true)
		} else {
			online := []backend.Device{}
			for _, d := range v.devices {
				if d.Connected && d.SMSCapability == "available" {
					online = append(online, d)
				}
			}
			if m.deviceID == "" && len(online) == 1 && !m.busy {
				m.deviceID = online[0].ID
				c := m.cfg
				c.KDEConnect.PreferredDevice = m.deviceID
				return m, tea.Batch(m.saveConfig(c), refreshTick(), m.startSession(false), m.loadCache(), m.maybeSyncContacts(), m.processQueue(), m.refreshCounts())
			}
			if m.deviceID == "" && len(v.devices) > 0 && !m.promptedDevices && m.modal == "" {
				m.promptedDevices = true
				m.openDevices()
			}
			if len(v.devices) == 0 {
				m.notify("No paired phones — pair Android in KDE Connect, then press r", true)
			}
		}
		return m, tea.Batch(refreshTick(), m.startSession(false), m.maybeSyncContacts(), m.processQueue(), m.refreshCounts())
	case refreshMsg:
		if !m.discovering && !m.quitting {
			return m, m.discover()
		}
		return m, refreshTick()
	case debounceMsg:
		if !m.quitting {
			if d := m.drafts[v.phone]; d.Revision == v.revision {
				return m, m.saveDraft(v.phone, d)
			}
		}
		return m, nil
	case savedMsg:
		if v.err != nil {
			m.notify("Draft could not be saved — keep TideSMS open and retry", true)
			m.logError("save draft", v.err)
		}
		return m, nil
	case localProvidersMsg:
		m.localProviderStatus = v
		return m, nil
	case aiModelsMsg:
		return m, m.applyAIModels(v)
	case configMsg:
		lookupAfterKey := m.lookupAfterKey
		m.lookupAfterKey = false
		m.busy = false
		if v.err != nil {
			m.notify("Could not save configuration; change was not applied", true)
			m.logError("save config", v.err)
		} else {
			oldDevice := m.deviceID
			m.cfg = v.cfg
			// Saving an unrelated setting must not deselect the phone: a config
			// that names no preferred device leaves the current one alone.
			if d := v.cfg.KDEConnect.PreferredDevice; d != "" {
				m.deviceID = d
			}
			m.editor.SetMode(v.cfg.Composer.Mode)
			m.assistant = buildAssistant(v.cfg)
			if oldDevice != m.deviceID && m.history.enabled {
				m.history.active = nil
				m.history.view = conversation.New()
				m.recipient = contacts.Contact{}
				m.editor.SetValue("")
				m.setPane(paneThreads)
				return m, tea.Batch(m.startSession(true), m.loadCache())
			}
			m.layoutConversation()
			if lookupAfterKey && m.modal == "settings" {
				m.selectSettingRow(settingAIModel)
				return m, m.chooseAIModel()
			}
		}
		return m, nil
	case mutationMsg:
		m.busy = false
		if v.err != nil {
			m.notify("Could not save contact; check storage and retry", true)
			m.logError("contact mutation", v.err)
			return m, nil
		}
		if v.deleted != "" {
			for i, c := range m.contacts {
				if c.ID == v.deleted {
					m.contacts = append(m.contacts[:i], m.contacts[i+1:]...)
					break
				}
			}
			if m.recipient.ID == v.deleted {
				m.recipient.ID = ""
				m.recipient.Name = m.recipient.PhoneNumber
				m.recipient.Theme = ""
			}
		}
		if v.contact != nil {
			found := false
			for i, c := range m.contacts {
				if c.ID == v.contact.ID {
					m.contacts[i] = *v.contact
					found = true
				}
			}
			if !found {
				m.contacts = append(m.contacts, *v.contact)
			}
			// Editing the contact behind the open thread must not close it or
			// discard its draft; only a genuinely new recipient starts over.
			if m.history.active != nil && m.recipient.PhoneNumber == v.contact.PhoneNumber {
				m.recipient = *v.contact
				m.layoutConversation()
			} else {
				m.choose(*v.contact)
			}
		}
		sort.SliceStable(m.contacts, func(i, j int) bool { return strings.ToLower(m.contacts[i].Name) < strings.ToLower(m.contacts[j].Name) })
		m.selected = max(0, min(m.selected, len(m.filtered())-1))
		m.modal = ""
		m.notify("Contact saved", false)
		return m, nil
	case sentMsg:
		m.sending = false
		m.sendPhone = ""
		m.setFocus(m.focus)
		if v.err != nil {
			m.notify(v.err.Error(), true)
			return m, nil
		}
		m.notify("✓ Submitted to "+v.name+" · delivery unverified", false)
		d := m.drafts[v.phone]
		if d.Revision == v.revision && d.Body == v.body {
			d.Body = ""
			d.Revision++
			m.drafts[v.phone] = d
			if m.recipient.PhoneNumber == v.phone {
				m.editor.SetValue("")
			}
			return m, m.saveDraft(v.phone, d)
		}
		return m, nil
	case quitMsg:
		if v.err != nil {
			m.quitting = false
			m.notify("Could not save drafts; quit cancelled. Fix storage and try again.", true)
			return m, nil
		}
		return m, tea.Quit
	case keys.Action:
		switch v {
		case keys.ActionSchedule:
			return m, m.openSchedule()
		case keys.ActionNewline:
			if m.modal == "" && m.focus && !m.sending {
				return m, m.insertNewline()
			}
		}
		return m, nil
	case tea.KeyMsg:
		if m.quitting {
			return m, nil
		}
		if m.modal != "" {
			return m, m.modalKey(v)
		}
		if m.searching {
			return m, m.searchKey(v)
		}
		if m.history.enabled && m.history.search {
			return m, m.conversationKey(v)
		}
		if v.String() == "ctrl+p" {
			m.openPalette()
			return m, nil
		}
		// Ctrl+G reviews the draft. A pending request is cancelled first so a
		// second press always reflects the current text.
		if v.String() == "ctrl+g" {
			m.cancelAI()
			return m, m.startAI("review", "")
		}
		if v.String() == "ctrl+f" {
			m.openGlobalSearch()
			return m, nil
		}
		// Settings are reached from every pane, the composer included, which is
		// why this is a chord: a bare key there is text. Ctrl+O is free of the
		// terminal's own meanings, unlike Ctrl+S, which many still read as flow
		// control.
		if v.String() == "ctrl+o" {
			return m, m.openSettings()
		}
		// Enter submits from the composer (below); Ctrl+Enter and F12 are the
		// explicit keys that work from any pane and whatever the terminal
		// reports for a bare Enter.
		if v.String() == "f12" || v.String() == "ctrl+enter" {
			return m, m.send()
		}
		// Scheduling is also on the palette; this supports terminals that can
		// distinguish Ctrl+Shift+Enter.
		if v.String() == "ctrl+shift+enter" {
			return m, m.openSchedule()
		}
		if m.history.enabled {
			switch v.String() {
			case "alt+1":
				m.focusArea(paneThreads)
				return m, nil
			case "alt+2":
				m.focusArea(paneConversation)
				return m, nil
			case "alt+3":
				m.focusArea(paneComposer)
				return m, nil
			}
		}
		if v.String() == "alt+esc" {
			if m.history.enabled && !m.focus {
				if m.history.pane == paneConversation {
					return m, m.conversationKey(tea.KeyMsg{Type: tea.KeyEsc})
				}
				m.setPane(paneThreads)
				return m, nil
			}
			return m, m.leaveComposer()
		}
		if v.String() == "tab" || v.String() == "shift+tab" {
			m.cyclePane(v.String() == "shift+tab")
			return m, nil
		}
		if m.focus {
			if m.sending {
				return m, nil
			}
			// Enter sends when [composer] enter_sends is on, as a phone does.
			// Shift+Enter inserts a newline (handled above), and Alt+Enter is
			// the fallback for terminals that cannot report the shift.
			if m.cfg.Composer.EnterSends && v.String() == "enter" {
				return m, m.send()
			}
			if v.String() == "alt+enter" {
				if m.cfg.Composer.EnterSends {
					return m, m.insertNewline()
				}
				return m, m.send()
			}
			// Plain editing has no use for Esc, so it leaves the composer. Vim
			// keeps Esc for its modes, and leaves on a clean second Esc instead.
			if v.String() == "esc" && m.cfg.Composer.Mode != "vim" {
				return m, m.leaveComposer()
			}
			if m.recipient.PhoneNumber == "" && m.history.active == nil {
				m.notify("Choose a recipient first · n for a number", true)
				return m, nil
			}
			return m, m.updateEditor(v)
		}
		if m.history.enabled && m.history.pane != paneContacts {
			return m, m.conversationKey(v)
		}
		return m, m.navigation(v)
	}
	if m.focus && !m.sending && m.modal == "" && m.loaded {
		return m, m.updateEditor(msg)
	}
	return m, nil
}
func refreshTick() tea.Cmd {
	return tea.Tick(8*time.Second, func(time.Time) tea.Msg { return refreshMsg{} })
}
func (m *Model) logError(op string, err error) {
	if m.log != nil {
		m.log.Error(op, "error", err.Error())
	}
}

// insertNewline adds a newline to the draft as one undo unit.
func (m *Model) insertNewline() tea.Cmd {
	m.editor.InsertString("\n")
	return m.trackChange()
}

func (m *Model) updateEditor(msg tea.Msg) tea.Cmd {
	cmd, err := m.editor.Update(msg)
	if err != nil {
		m.notify("Clipboard unavailable; check wl-clipboard or xclip", true)
	}
	var wrapped tea.Cmd
	if cmd != nil {
		epoch := m.editorEpoch
		wrapped = func() tea.Msg { return editorMsg{epoch, cmd()} }
	}
	return tea.Batch(wrapped, m.trackChange())
}

// FlushDrafts is called after the event loop stops, including signal shutdown.
func (m *Model) FlushDrafts() error {
	if m.store == nil {
		return nil
	}
	for p, d := range m.drafts {
		if err := m.store.SaveDraft(p, d); err != nil {
			return err
		}
	}
	return nil
}
