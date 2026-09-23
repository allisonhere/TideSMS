# TideSMS status

Milestone 1 is implemented and its live SMS test passed on 2026-09-22.
Milestone 2 is implemented and verified against fixtures; its live phone test is still outstanding.

## Undo send and message runs

A sent message waits `[composer] undo_seconds` (default 4, **Settings → Undo send**) before leaving. The status line counts down; Esc takes it back with the draft intact and Enter sends at once. Quitting inside the window is refused. An offline phone skips the window and goes straight to the queue prompt.

Consecutive messages from one sender within five minutes share one header and sit without gaps. One-to-one incoming headers show only the time. Only the newest outgoing message shows a settled status. A message's shape is part of the layout cache key, so a cached message is redrawn when a new neighbour changes it.

Fixed: the composer placeholder lost its first letter (Ripple draws the cursor in place of it) and showed a cursor before the composer was focused; the status bar showed `INSERT` in plain editing mode and while other panes were focused, and now shows Vim's mode only while composing; selecting the last settings row in a short window scrolled it out of view.

Thread rows keep their time beside the unread count (`2 · 13:40`), show **Draft:** and the unsent text for any thread but the one being typed in, and preview a media-only message by its kind (`Photo`, `Video`, `2 attachments`) through `Message.Preview`. Rows cached before this with no text show `Attachment` until their next message. Verified in the real binary against the live phone.

Not yet verified by hand: a real send through the undo window.

## Keyboard pane navigation

Enter opens a thread ready to compose. Tab/Shift+Tab toggle the remembered sidebar and draft; Alt+1/2/3 focus threads/history/composer. Escape steps back through history to threads, with search/dialog dismissal first and Vim mode handling preserved. Read-only groups toggle sidebar/history. Existing headers and separators identify the focused area without extra rows.

## History rendering performance

Conversation layouts reuse unchanged text and image bubbles when older pages arrive. Width, theme, search, sender names, message changes, and attachment file changes invalidate cached output. Graphics transmissions are limited to visible messages. A 1,000-message text re-layout benchmark fell from roughly 28 ms to 1.1 ms on this machine.

## AI polish

**AI: Polish writing** combines spelling and grammar correction with sentence rewriting. It uses the existing preview, accept/reject, and undo flow. Rejecting a rewrite now preserves the original, and whole-draft rewrites are discarded if the draft changed while waiting.

## AI request status

Completed reviews and rewrites replace the pending status with “AI suggestions ready.” Cancelling clears the busy state immediately and ignores late responses, so “Asking the assistant” cannot remain after either transition.

## AI settings fix

API keys, endpoints, and model selections are saved separately for each provider. Switching providers restores that provider’s setup, including after restarting; existing flat AI settings are retained for the currently selected provider.

The AI toggle saves independently of provider availability or model setup and never selects Ollama automatically. A saved `ai.ollama_endpoint` is used for Ollama availability and restored when switching back from a cloud provider. Settings checks local providers in the background and marks unavailable choices grey with an explicit label; unavailable choices cannot be selected. API key precedes Model, and saving a nonempty cloud key requests the provider’s model list without message text. Writing privacy policy still applies to AI requests; it does not block explicit catalogue setup. Re-saving a key retries failed lookup, and rejected keys return to the API key row. Incomplete AI setup remains loadable, and AI actions explain missing configuration. Empty model edits close with Enter; Tab and arrows leave typed fields.

## Milestone 1 implementation

Implemented: two-pane TideUI shell; Ripple normal/Vim editing; clipboard integration; modified Enter support plus F12/palette fallback; contact add/edit/delete/search; manual recipients; automatic/explicit per-contact accents; global settings; paired-device discovery and selection; optional SMS plugin probe; async CLI submission; connection/send/error feedback; SQLite migrations and revision-protected per-recipient drafts; TOML persistence; private structured logs.

Verified with automated tests: backend parsing and literal subprocess arguments, offline/plugin rejection, content-safe logging, draft revision ordering and restart persistence, configuration round-trip and malformed-file preservation, key routing, contact forms/palette, failed/successful submission transitions, recipient switching during submission, and rendering bounds.

Verified against the local Galaxy S25 Ultra: paired, reachable, SMS plugin loaded (read-only integration test). A real SMS was submitted through the TideSMS composer with Ctrl+Enter; the unchanged draft cleared after CLI success, and the user confirmed receipt. The destination and message are intentionally omitted from this document.

Known CLI limitation: exit success is not itself a delivery receipt, and upstream currently does not check the SMS D-Bus error reply. The app therefore labels submissions as delivery-unverified; see README.md.

## Milestone 2 implementation

Implemented: `ConversationBackend` over the `org.kde.kdeconnect.device.conversations` D-Bus interface (thread discovery, paged history, `conversationCreated`/`conversationUpdated` signals, reachability and daemon-ownership changes), with the existing `kdeconnect-cli` send path retained; a transport-independent `internal/domain` thread/message/event model; an SQLite cache of threads, participants, messages and per-thread sync state that is the only source the UI renders from; a UI-independent sync engine with per-thread watermarks and bounded page replay; an adaptive three/two/one-pane layout; a thread list with previews, timestamps and unread badges; a conversation component with date separators, an unread boundary, direction-aware layout, width-safe wrapping, selection, copy, inspector and older-history loading; thread-scoped drafts with migration from number-keyed drafts; thread search over the cache with match highlighting; global → contact → thread accent resolution with deterministic group accents; local unread state with manual mark-unread; live incoming messages with a notification policy that stays silent for the visible thread; optimistic send with `sending`/`submitted`/`failed` and in-place retry; automatic resubscribe and catch-up after disconnection; new palette commands; sync and offline status reporting; and `[sync]`, `[notifications]` and `[conversation]` configuration with validated bounds.

Verified with automated tests, all driven by the deterministic fake backend rather than a live phone: incremental sync that re-downloads nothing when a watermark is unchanged and replays only to the watermark when it is not; fingerprint deduplication that collapses identical replays while keeping distinct same-timestamp messages; failed sync leaving the cache intact; cancellation; cached-first startup that makes no backend call before the first frame and preserves unread state across restart; contact resolution, unknown-number threads and group flags; direction rendering; read-on-open; group sending refused with an explanation; live messages reaching the open thread without a notification while background messages raise unread counts and notify, including the hidden-body option; send showing `sending`, failing without clearing the composer, and retrying in place without duplicating; thread-scoped drafts across switches and restart; search served from SQLite with the phone untouched; thread themes overriding contact themes and surviving restart, with stable group accents; offline browsing, drafting and search followed by automatic reconnect and catch-up within the same session; layout bounds and preserved selection at six widths; the message inspector and manual unread; and contact edits leaving the open thread and its draft intact.

Two defects found and fixed while testing Milestone 2: editing the contact behind an open thread closed that conversation and dropped its draft; and an unexportable KDE Connect device identifier surfaced a raw D-Bus `Invalid match rule` error instead of being refused with an explanation. KDE Connect filters identifiers to `[A-Za-z0-9_]` when generating them, so a real device is unaffected.

Alt+Enter was added as a first-class send key alongside Ctrl+Enter and F12, because window managers commonly bind Ctrl+Enter; on this machine Hyprland claims it.

Validation passed: build, go vet, race-enabled Go tests, golangci-lint (zero issues), and the isolated real-PTY smoke test, which now also covers the pane layout, Alt+Enter submission, the cached outgoing message, and thread-scoped draft restoration.

Not verified: no Milestone 2 code path has yet been exercised against a real phone. Thread discovery, history paging and live signals have only been run against fixtures, so the D-Bus quiet-period heuristics in `internal/backend/kdeconnect/conversations.go` and the message field decoding remain unconfirmed on real hardware. Run TideSMS against the paired phone and check thread contents, ordering, incoming delivery and reconnection before treating Milestone 2 as complete.

## Contact import (added after Milestone 2, by request)

Implemented: an optional `ContactsBackend` over `org.kde.kdeconnect.device.contacts`, which triggers `synchronizeRemoteWithLocal`, waits for `localCacheSynchronized`, and parses the plugin's vCard cache at `$XDG_DATA_HOME/kpeoplevcard/kdeconnect-<device>/`; a tolerant vCard parser covering 2.1 (quoted-printable, `=`-folded, CRLF, parameterless TEL types) and 3.0/4.0 (indented folding, grouped properties), with FN → N → ORG name fallbacks and control characters stripped; a per-device `synced_contacts` table; name resolution of local contact → synced contact → raw number; the overlay merged into the contact list after local entries and marked `⟲`; promotion to a local contact on edit or theme; deletion refused; a `Sync phone contacts` palette command plus one automatic import per phone per run; and a `[contacts] sync_from_phone` switch.

The design is a read-only overlay by the user's choice: synced entries never touch the contacts table, and a re-import replaces the overlay wholesale so a contact deleted on the phone disappears locally without affecting anything the user made.

Verified with automated tests: vCard parsing across both dialects including folding, quoted-printable UTF-8, multiple cards per file, duplicate and unusable numbers, and control-character stripping; cache reading that skips directories, wrong extensions, oversized and malformed files; XDG path resolution; thread names filled from the overlay while a local contact still wins; local contacts provably unmodified; re-import replacing rather than accumulating; list ordering, marking and search; promotion on edit and on theme; delete refused; failed imports quiet in the background and reported when requested; one import per phone; and the configuration switch.

Verified against the phone on 2026-09-22: 64 vCards imported as 81 numbers, naming 30 of the 72 cached threads. Two approvals are required on the Android side and only the first is discoverable from Android's settings: the `READ_CONTACTS` permission, and a per-device `acceptedToTransferContacts` confirmation that `ContactsPlugin.checkRequiredPermissions()` also requires. Until the second is accepted the plugin advertises contacts capability and receives the request but returns nothing, with no error on either side; the daemon log shows only repeated `sendRequest: Sending "kdeconnect.contacts.request_all_uids_timestamps" true`. The error messages now name that confirmation.

Number formats differ between the address book and SMS addresses: on this phone 48 of 81 contact numbers are national-format while 64 of 82 thread addresses are E.164, so exact matching alone named just 15 threads. Matching now falls back to the trailing ten digits when exactly one contact shares them, which took real coverage from 15 to 30 threads with no ambiguous cases in the live data. Exact matches still win, and ambiguity is left unresolved rather than guessed.

Earlier partial verification: the D-Bus object path, interface and `synchronizeRemoteWithLocal` call all work against the paired Galaxy S25 Ultra — the call is accepted — but the phone emits no `localCacheSynchronized` and its vCard cache stays empty, because the KDE Connect Android app has not been granted the Contacts permission. The parser and storage path therefore remain unexercised on real data. Grant Contacts in the KDE Connect Android app, then run:

```sh
TIDESMS_TEST_DEVICE=<device-id> go test ./internal/backend/kdeconnect -run TestLiveContacts -v
```

## Contact list scope and new-message search (by request)

The imported address book is no longer listed wholesale. The contacts sidebar shows the user's own contacts plus the imported people they actually have a conversation with; on the live data that is 31 of 81 imported numbers instead of all 81. Everyone else is reached through **n**, which opens a search over the whole address book and also offers a typed number directly, replacing the old number-entry form. Choosing someone who already has a thread opens it instead of starting a second one, matching numbers tolerantly so a contact stored as `8165550182` finds a thread whose address arrived as `+18165550182`.

Verified with automated tests: the sidebar hides an imported contact with no conversation while the address book still holds it; the picker lists everyone, searches names and numbers, offers a full typed number and refuses an unusable one, opens an existing thread rather than duplicating it, and matches an existing thread across number formats; and the picker stays within bounds at every adaptive width.

## Group detection corrected (found in a screenshot of real use)

The thread group flag came from KDE Connect's `EventMultiTarget` bit, which the phone also sets when it lists one person's address more than once, or in two formats. On the live database that mislabelled 19 of 21 "group" threads: each showed a single participant, replaced 172 message senders with `Group participant`, and — because group sending is refused — made those conversations unanswerable.

Addresses are now deduplicated at decode time by their national portion, so the same person written twice collapses to one participant, and a thread is a group when it has more than one distinct participant. The flag is recomputed from the participants actually known whenever a thread is merged, so a thread mislabelled by one message corrects itself. Migration 005 recomputes it for existing rows and restores the sender on messages in single-participant threads. On the live database this took group threads from 21 to 4, made 19 conversations answerable, and left only the 33 anonymous senders that belong to real groups.

Related display fixes from the same screenshot: a thread whose participants resolve to one name no longer repeats it ("Allie Bayless, Allie Bayless, Allie B"); long thread names are elided instead of butting against the timestamp; and the group header no longer reads "David Queen · David Queen · 1 participants".

Verified with automated tests: one address, the same address twice, and the same number in two formats all stay one-to-one with the real sender preserved, while two distinct numbers remain a group; the stored flag follows the participants known and upgrades to a group when a second person appears.

## Sender names in a conversation

An incoming message printed `msg.Sender` verbatim, which is the raw address. The thread title resolved the contact but every message line in the conversation showed the number, so a thread titled "David Queen" was a column of `+13145178351`. Layout now takes the participant names the store already resolves — local contact, then imported contact, then the number — keyed by both the normalized number and its national portion, so a contact stored without a country code still names its messages. Names are stripped of control characters like every other value from the phone.

Separately verified on the live thread: all 100 cached messages reassemble from the rendered lines exactly as stored, so wrapping loses, duplicates and reorders nothing.

## Sender names and pane layout

An incoming message printed `msg.Sender` verbatim, which is the raw address, so a thread titled "David Queen" was a column of `+13145178351` even though the title itself resolved. Layout now takes the participant names the store already resolves — local contact, then imported contact, then the number — keyed by both the normalized number and its national portion, and strips control characters as every other value from the phone is.

The contact list no longer holds a permanent column. Threads carry resolved names, so it shares the sidebar and appears only on **c**, returning on **Esc**; Tab skips it. On a 177-column terminal this took the conversation pane from 96 to 126 columns.

Separately verified on the live thread: all 100 cached messages reassemble from the rendered lines exactly as stored, so wrapping loses, duplicates and reorders nothing.

## Themes from TideUI, assignable per contact

The theme system used seven hand-written accent colours over a single fixed palette. It now offers TideUI's nineteen built-in themes through `tideui.BuiltinThemes` and `ThemeByName`, and any of them can be assigned to a contact or a thread. An explicit theme is applied whole — background, foreground, borders, status bar — while a contact without one keeps the global palette and takes only a derived accent, so automatic differentiation no longer repaints the interface.

A contact's or thread's theme is scoped to the conversation. The shell — thread list, contact list, status bar, modals — is drawn by a renderer built from the global theme alone, while the conversation pane's contents and its border accent come from a second renderer that resolves the contact and thread overrides. Opening a themed conversation therefore recolours that pane and nothing else.

The seven old names remain valid and resolve to an accent over TideUI's default palette, so configurations and contacts written earlier keep working untouched; the live configuration's `mint` is one of them.

The conversation's pane body is painted with its own background through TideUI's `StyleOver`, which reopens the colour after the inner styles' resets, so a themed conversation carries the whole palette rather than only its text colours.

Theme pickers preview live: the renderers consult the highlighted choice while a picker is open, so the conversation repaints under the contact and thread pickers and the whole interface under the global one, without anything being written until the choice is confirmed.

Verified against the live database with a thread set to `gruvbox-dark` under a `nord` global: the shell painted 395 cells of #2e3440 while the conversation painted 56 of #282828, and highlighting `coral-sunset` in the thread picker drew #444154 immediately while the shell stayed `nord`.

Verified with automated tests: every offered theme is accepted and resolves to itself; an explicit theme replaces the whole palette rather than an accent; automatic accents leave the palette alone, stay stable across runs and spread across the accent set; and all seven legacy names still resolve with their original colour, including as a global theme.

## Message frames

Messages are drawn in a frame, as the milestone sketch showed, with the sender and time above and the outgoing status below. `[conversation] bubbles` controls it and **Open settings → Toggle message bubbles** flips it live; the setting persists. The frame's four cells come out of the text width, so a message occupies the same space either way, and a pane too narrow to close a frame falls back to the gutter bar rather than drawing a broken one.

`[conversation] fill_bubbles` and **Toggle bubble fill** paint a frame and its inside. Only the frame is painted: the sender, the status and the space beside a bubble keep the pane's own background, and the colour is reopened after inner styles' resets so the fill is unbroken across highlighted search matches.

The two directions fill differently. A received message uses the theme's raised surface (`Overlay`, with `StatusBar` then `Bg` as fallbacks). A sent message cannot use the next surface along, because sixteen of the nineteen themes define `StatusBar` identical to `Overlay`, so its fill is derived: the background is shifted towards the accent in Lab space **with the background's lightness preserved**. Blending normally cost contrast — `tokyo-night-day` fell from its own 4.52 to 3.11, and `one-dark` to 3.87 — whereas preserving lightness changes hue alone, leaving every theme within 0.04 of its own background/text ratio except two already above 13:1. The tint still backs off in steps if a theme somehow loses contrast, and is never held to a ratio the theme does not meet itself.

Verified across all nineteen built-in themes: the two fills are always distinct, received messages use the raised surface, and the sent fill is never measurably less legible than the background it replaces.

Corners are round or square, chosen by `[conversation] corners` or **Toggle bubble corners**; an unset or unrecognised value stays round. Both styles occupy the same space.

Verified with automated tests: frames close on all four sides at every width that can hold them, never exceed the pane, are absent where they cannot fit, and carry exactly the same text as the plain style; the settings toggle changes the rendering immediately and is written to disk.

Found while testing it: saving any setting blanked the selected phone whenever the configuration named no preferred device, which closed the open conversation and cleared its recipient. A saved configuration that names no device now leaves the current selection alone.

Two test traps found and fixed while verifying the colours: lipgloss strips styling when no terminal is attached, so colour assertions passed vacuously until the tests set a true-colour profile and restored it afterwards; and lipgloss round-trips some hex values a shade off (`#313244` renders as rgb 48,50,68, not 49,50,68), so the expected escape is now taken from a real render instead of computed from the hex. The earlier background tests had passed only because their colours happened to round-trip exactly.

## Composer chrome

The line above the composer repeated the editing mode and the editor's name, which the status bar already carries. It is gone, and the row goes to the conversation. That line is now used only when there is something to say about sending — currently that a group is read-only — and the conversation gives up a row only then.

## Message layout

Message width was capped twice over: at four fifths of the pane and again at a fixed ceiling, on top of margins that were already reserved. On a wide terminal the ceiling dominated, so messages wrapped at about seventy columns however large the window was and left a wide column of the conversation permanently empty. Raising the ceiling from 70 to 72 changed nothing visible, which is what made the report hard to pin down. Bubbles now take the pane minus a small fixed gap opposite them, and `[conversation] max_width` caps that for anyone who wants a narrower measure. On a 177-column terminal the sample thread's messages went from two wrapped lines each to one.

Bubbles also reserve the selection marker, their gutter and a right margin explicitly, so text no longer touches the pane border on the outgoing side while sitting well inside on the incoming side, and the sender, body and status align to one right edge. A test asserts no rendered line exceeds the pane at ten widths from 120 down to 1, over emoji, ZWJ sequences, CJK, combining marks, embedded newlines and unbreakable URLs, and that wrapping never loses text.

## Not implemented

No MMS or attachment rendering (attachments are marked in the body and not displayed), no scheduled sending, no offline send queue, no automatic retry, no phone-side read-state synchronization, no pinned threads, and no AI features.
