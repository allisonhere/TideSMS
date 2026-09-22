# TideSMS

A keyboard-first terminal SMS client built with [TideUI](https://github.com/allisonhere/tideui) and [Ripple](https://github.com/allisonhere/ripple). It reads your phone's SMS threads over KDE Connect, caches them in SQLite, follows new messages live, and replies from a threaded conversation view. MMS rendering, scheduled sending, an offline send queue, and AI features are not implemented.

## Run

Requirements: Go 1.26.1 or newer to build, `kdeconnect-cli`, and a paired Android phone. Run TideSMS in your desktop session, with the KDE Connect daemon available. Enable the SMS plugin and grant its Android permissions. `busctl` (normally provided by systemd) enables the optional read-only SMS capability check. Clipboard operations use the system clipboard through `wl-copy`/`wl-paste`, `xclip`, or the platform equivalent.

```sh
make build
./tidesms
```

The binary is built in this directory; nothing is installed system-wide. `./tidesms --help` lists path overrides, and `./tidesms --version` prints the version.

1. The app selects the only connected device with a loaded SMS plugin, or opens a device selector. Use **Ctrl+P → Switch device** to change it. The selector shows device ID, reachability, and SMS capability.
2. Press **n** to start a message. That opens a search over your whole address book — your own contacts and everyone imported from the phone — and typing a full number offers that number directly, so an unknown recipient needs no separate step. Press **a** to save a contact. Phone numbers accept common formatting; an international prefix is recommended. No country code is guessed.
3. TideSMS opens on the **Threads** pane, populated from the local cache before the phone answers. **Enter** opens a thread and focuses the composer, so you can reply straight away. **c** borrows the sidebar for the contact list, where **Enter** on a contact opens their thread if one exists, and **Esc** returns to threads.
4. Press **Enter** on a contact or thread, then compose. **Enter submits** and **Shift+Enter inserts a newline**; **Ctrl+Enter**, **F12** and **Ctrl+P → Send message** are equivalent explicit send actions. With `[composer] enter_sends = false`, Enter inserts a newline and those send instead.
5. **Esc** leaves the composer (in Vim, **Alt+Esc** or a clean second **Esc**), returning to the conversation — or to the thread list when the recipient has no thread yet. Switching threads retains each thread's draft. Use **t** to change a contact's accent, or **Ctrl+P → Change thread theme** for this thread only. A theme shows in the sidebar as well as in the conversation: a threaded or contact row draws its name in that palette's accent, so people are distinguishable before their conversation is open. A thread uses its own theme if it has one and the contact's otherwise; a group, belonging to no single contact, shows only a theme set on the thread. The selected row is left in the selection's own colours, where an accent chosen for the pane background has no contrast guarantee.

## Conversations

The layout is threads plus conversation from 70 columns up, and a single focused pane below that. The contact list is not a permanent third column: threads already carry resolved names, so the list shares the sidebar and appears only when you press **c**, with **Esc** returning to threads. That keeps the full remaining width for the conversation. **Tab** and **Shift+Tab** cycle panes and preserve each pane's selection and scroll position across width changes.

A conversation shows date separators, an unread boundary, incoming and outgoing messages on opposite sides, the sender's resolved name, per-message timestamps, and the status of each outgoing message. **Enter** on a message opens its details and actions: Copy, Reply, Quote, Search text, Delete local copy, and Retry on a failed submission. **Quote** inserts the message into the composer as `> `-prefixed text — SMS has no reply-to metadata, so quoting is plain composition and can be undone like any edit. **Delete local copy** removes only the cached row; the phone's copy is untouched and a synced message may reappear after the next sync. The composer's length and SMS segment estimate are shown beside it, computed for GSM-7 or UCS-2 as the text requires. Messages grow with the pane, keeping only a small gap on the opposite side so the two directions stay easy to tell apart; set `max_width` if you prefer a narrower measure on a very wide terminal. Each message is drawn in a frame, which **Ctrl+P → Open settings → Message bubbles** turns off in favour of a single gutter bar, **Bubble corners** switches between round and square, and **Bubble fill** paints the inside of each frame. Received and sent messages fill differently: a sent message sits on the theme's raised surface, a received one on the background shifted towards the accent. All three settings persist. A pane too narrow to close a frame uses the bar regardless.

```text
David Queen · 07:24
╭─────────────────────────────────────────────╮
│ Morning read once hc drops if you die you   │
│ can transfer to a different kind of realm   │
╰─────────────────────────────────────────────╯

                                   You · 07:25
              ╭──────────────────────╮
              │ it works thatway now │
              ╰──────────────────────╯
                                           sent
``` Bodies wrap by display width, so emoji and non-Latin text are not split mid-character. **j/k** select a message, **Enter** opens the inspector, **y** copies the body, **r** replies (and prepares a failed message for retry), and **g/G** jump to the oldest loaded or newest message. Scrolling near the oldest loaded message loads another batch from the cache and, when the phone is reachable, from the phone.

Selecting a thread points Ripple at it automatically; the composer header names who you are replying to. A thread counts as a group only when it has more than one distinct participant: phones often list the same person's address twice, or in two formats, and those collapse to one person rather than making a conversation unanswerable. Groups are read-only: their history is displayed and searchable, and sending is refused with an explanation, because the KDE Connect interface used here addresses a single destination number.

**/** searches the open thread. The search runs against SQLite, never the phone; **n** and **N** step through matches and **Esc** restores the full thread.

## Contacts from the phone

**Ctrl+P → Sync phone contacts** imports your phone's address book through KDE Connect's contacts plugin, and TideSMS also imports once automatically the first time it connects to a phone. Imported entries fill in names for threads that would otherwise show a bare number, in the thread list, the conversation header and desktop notifications.

The contacts sidebar deliberately does not list the whole imported address book, most of which is people you have never texted: it shows your own contacts plus the imported people you actually have a conversation with. Everyone else stays one keystroke away through **n**, the new-message search. Choosing someone who already has a thread opens it rather than starting a second one, matching numbers tolerantly.

Imported entries are a read-only overlay. They live in their own table, are listed after your own contacts and marked `⟲`, and are never written into the contacts you created. Resolution runs local contact → synced contact → raw number, so a local contact always wins, and a re-import can never rename, re-theme or delete anything you made. Re-importing replaces the overlay wholesale, so a contact removed on the phone disappears here too.

To theme a synced entry or change how it is stored, press **e** (edit) or **t** (theme) on it: that keeps a local copy you own, which then shadows the phone's version. **d** (delete) is refused, because the phone owns that entry — remove it on the phone instead.

Requirements on the phone: **two** separate approvals, which is easy to miss. The Android **Contacts permission** must be granted to KDE Connect, *and* the Contacts plugin needs a **per-device transfer confirmation** — in the KDE Connect Android app, open this desktop's plugin settings, tap the Contacts row itself and accept the dialog. Until that second approval is given, the plugin still advertises contacts support and still receives the request, but silently sends nothing back; the import reports this rather than failing obscurely. Set `sync_from_phone = false` to disable the feature entirely; nothing is then read or stored.

The plugin caches vCards under `~/.local/share/kpeoplevcard/kdeconnect-<device-id>/`; TideSMS reads that cache and stores only the names and numbers it needs. Android emits vCard 2.1 with quoted-printable text, which is parsed alongside 3.0 and 4.0.

## Synchronization and offline behavior

Cached threads render before any phone call is made. Synchronization then runs in the background and updates the view incrementally; the interface is never blanked while it runs. The status bar reports `Syncing N threads…`, then `Synced HH:MM`, and the phone's reachability separately.

Each thread records the last backend message id it has seen. A thread whose latest id is unchanged is not downloaded again; a thread with new activity is replayed only back to that watermark. Messages are keyed by their KDE Connect id where one exists and otherwise by a deterministic fingerprint over thread, sender, timestamp, body and direction, so repeated syncs cannot duplicate a message. Timestamps alone are never treated as unique.

New messages arrive through KDE Connect's `conversationCreated`/`conversationUpdated` signals rather than polling. If the daemon disappears or the phone drops off Wi-Fi, the subscription is restored automatically and the missed messages are collected; no restart is needed. While the phone is away you can still browse threads, read cached history, search, and write drafts. Sending fails cleanly and keeps the message for retry.

Unread state is local. A thread is marked read when you open it and are at the newest messages, never merely because the app started. **Ctrl+P → Mark thread unread** restores the badge.

A message that arrives in the thread you are looking at updates it silently. One that arrives elsewhere raises that thread's unread count and posts a desktop notification. The title is the sender unless `show_sender = false`; the body is the message text unless `show_body = false` or `privacy = true`, in which case it says "New SMS". **Ctrl+P → Mute thread** silences a conversation and **Unmute thread** restores it; the override is stored per thread and beats a per-contact one, and **Toggle notification body preview** flips the global body setting.

## Keys

| Context | Key | Action |
|---|---|---|
| Contacts | j/k, arrows | Move one contact |
| Contacts | PgUp/PgDn, Ctrl+U/D | Move a page |
| Contacts | g/G, Home/End | First/last contact |
| Contacts | Enter | Select and compose |
| Contacts | / | Search names and phone numbers |
| Contacts | a / e / d | Add / edit / delete (with confirmation) |
| Contacts | n / t / r | New message (searches everyone) / contact theme / refresh devices |
| Contacts | ⟲ rows | Entries imported from the phone; e or t keeps a local copy |
| Contacts | ? / q | Help / save drafts and quit |
| Threads | j/k, arrows | Move one thread |
| Threads | g/G, Home/End | First/last thread |
| Threads | Enter | Open the conversation and start typing (composer focused) |
| Threads | r / c / n | Refresh conversations / contacts / new message |
| Conversation | j/k, arrows | Select previous/next message |
| Conversation | PgUp/PgDn, Ctrl+U/D | Scroll; near the top loads older history |
| Conversation | g / G | Oldest loaded / newest message |
| Conversation | Enter | Message inspector |
| Conversation | y / r | Copy body / reply (retry a failed message) |
| Conversation | v | Open the selected message's image (real image where the terminal draws one) |
| Conversation | / then n/N, Esc | Search this thread, step matches, exit |
| Any pane | Tab / Shift+Tab | Cycle threads / conversation / composer |
| Contacts | Esc / c | Return to the thread list |
| Any pane | Ctrl+P | Searchable command palette |
| Composer | Enter | Submit SMS (see `enter_sends`) |
| Composer | Shift+Enter / Alt+Enter | Insert a newline |
| Composer | Ctrl+Enter / F12 | Submit SMS (equivalent) |
| Composer | Esc | Leave composer (Vim: Ripple owns Esc) |
| Composer | Alt+Esc | Leave composer, always, including in Vim |
| Composer | Ctrl+G | AI review of the draft (or the selection) |
| Composer | Ctrl+Shift+Enter | Schedule the message (also Ctrl+P → Schedule message) |
| Any pane | Ctrl+F | Search every cached message |

Ripple owns editor movement, wrapping, selection, copy/paste, multiline text, and undo/redo. Normal mode uses Shift+movement, Ctrl+arrows, Ctrl+C/X/V, and Ctrl+Z/Y. Vim mode supports Normal, Insert, Visual and Visual-line modes, motions/operators, and `u`/`Ctrl+R`. **Ctrl+C copies while the composer is focused.** App navigation never consumes ordinary Vim keys. Ripple's `:q` intent leaves the composer; `:w` does not submit. Sending stays on the explicit application commands.

By default **Enter submits** and **Shift+Enter inserts a newline**, as a phone messaging app does; **Ctrl+Enter** and **F12** also submit, and **Alt+Enter** inserts a newline for terminals that cannot report the shift. Set `[composer] enter_sends = false` to go back to Enter-as-newline, where **Ctrl+Enter**, **F12** and **Alt+Enter** submit. Distinguishing a modified Enter requires a terminal that reports it: TideSMS requests Kitty keyboard disambiguation and xterm modifyOtherKeys and handles their modified-key reports. Some terminals/multiplexers collapse modified Enter; with `enter_sends = false` nothing is lost, and F12 or the palette's **Send message** always work. Protocol settings are restored on exit. At very small sizes the app asks for a terminal of at least 54×16; drafts are retained.

## AI writing assistant

The assistant is a reviewer, not a chat pane. It helps you write the message you already intend to send, and it never sends, chooses a recipient, or edits the draft silently.

- **Ctrl+G** (or **Ctrl+P → AI: Review writing**) asks for spelling, grammar and punctuation corrections. Proposed changes open in a review modal: **a** Accept, **r** Reject, **e** Edit suggestion, **n**/**p** Next/Previous, **A** Accept all, **Esc** Close. Each change is shown individually, so a small correction reads as `their → there`.
- Rewrite actions — **AI: Fix spelling**, **Fix grammar**, **Clean up**, **Make shorter**, **Make friendlier**, **Make professional**, **Make clearer** and **Custom rewrite…** — run against a Ripple selection when there is one, otherwise the whole draft.
- Accepting a change replaces the text through Ripple's own edit path, so one undo (Vim `u`, or Ctrl+Z) restores exactly what was there before.
- While a review is open, suggested spans are marked in the composer with an accent underline. Set `inline_marks = false` in `[ai]` (or leave it) to turn the marks off.
- If the provider is unreachable, times out, or returns something malformed, the draft is left untouched and a notice says so. A request in flight is cancelled by starting another or leaving.

Privacy is enforced per conversation. **Ctrl+P → Change AI policy** (contact) and **Change thread AI policy** (thread) choose `inherit`, `local`, `cloud` or `disabled`; a thread override beats a contact override, which beats `[ai] default_policy`. A `local` policy never falls back to a cloud provider — the request is refused instead. The assistant is only built when `[ai] enabled = true`, and message text is never logged.

## Offline queue and scheduled send

- Sending while the phone is offline offers **Queue for later**, **Keep draft** or **Cancel**. Queued messages live in SQLite and are sent oldest-first once the phone reconnects. Every attempt is guarded by an atomic claim, so a duplicate reconnect event cannot send the same message twice, and retries back off and stop at `[queue] max_attempts`.
- **Ctrl+Shift+Enter**, or **Ctrl+P → Schedule message**, offers Send now, In 30 minutes, This evening, Tomorrow morning or a custom date/time. Scheduling stores an absolute instant: a scheduled message never leaves early because the phone reconnected.
- **Ctrl+P → Open outgoing queue** lists queued and scheduled messages together. **Enter** inspects, **s** sends now, **e** loads it back into the composer, **d** removes it and **p** pauses or resumes a queued item.
- The status bar shows `N queued` and `N scheduled` when either is non-zero.

The queued, sending, sent, failed and paused states are persisted, and a message caught mid-send when the process dies is paused rather than retried blindly. The TUI drains the queue itself while it is open; the optional `tidesms-daemon` does the same while it is closed, and both share `internal/queue`, `internal/scheduler` and `internal/messaging`.

## Media and attachments

Media is capability-gated: the interface only offers what the backend reports it can carry. `backend.Capabilities` records `SendText`, `ReceiveText`, `Groups`, `ReceiveMedia`, `SendMedia`, `DeliveryStatus` and `ContactSync`; media sending and delivery receipts stay false for KDE Connect, while receiving media is on.

A message with media shows a compact block after its body — `[ image: dinner.jpg ]`, the dimensions and size, and a `d download` or `v preview` hint — and the conversation never waits on a download. KDE Connect ships a small preview (100×100) inline with the message list and keeps the part itself behind a separate request, so the two are tracked apart: the preview is what the conversation can draw immediately, and the part is what `d` and `v` fetch. A preview never counts as downloaded — treating it as the file is what would leave the image that was actually sent out of reach. The best image on disk is drawn **inline in the message** — the part once fetched, otherwise KDE Connect's preview, so a thread shows its pictures straight away. A terminal that speaks the Kitty graphics protocol (Kitty, Ghostty, WezTerm) draws the real image, placed into the text grid with Unicode placeholders so it pads, frames, scrolls and clips exactly like a line of text; every other terminal gets **braille dots** (a 2×4 sub-pixel grid per cell, tinted with the local colour), which keeps detail at a small footprint and needs no graphics support at all. Either way the thumbnail is capped so the conversation around it stays readable, and **Open settings → Inline images** (or `[conversation] inline_media = false`) turns that off in favour of the block. The Kitty/iTerm2 full-screen view is still there on **v** for a larger look. **v** on the selected message means “show me the image”: a part already on disk opens at once, and one that is still only a preview is downloaded first and then opened, so the full-screen view never shows a 100×100 thumbnail blown up. **Enter**, then **View attachment**, reaches the same viewer through the message actions. The part starts as metadata only; **d** downloads it on demand through KDE Connect's `requestAttachmentFile`, and the daemon's cached file (`~/.cache/kdeconnect.daemon/<device>/`) becomes the local copy. Then **←/→** move between parts, **v** fetches the part if needed and draws it, **o** opens it externally (after an explicit confirmation), **s** copies it to the download directory without overwriting, and **c** copies its path. **o**, **s** and **c** act on the part alone and say so while only a preview exists, since they hand a real path to something else.

**v** opens the image at full quality: the TUI suspends and the same binary draws it with the Kitty graphics protocol (Kitty, Ghostty, WezTerm) or iTerm2 inline images, detected from the environment. It is drawn at its own proportions and never above its own resolution — given the whole screen a terminal stretches the image to fill it, which shows a small attachment blown up and soft rather than as it is — so a large photo is scaled down to fit and a small one is shown pixel for pixel; without graphics support it falls back to a large braille rendering, so the key always does something. This runs in a child process because TideUI panes pad every line, which would erase a raw graphics escape composed inside them. Files are never opened or executed automatically, and a part with no local copy simply cannot be opened rather than failing.

KDE Connect hands the thumbnail as base64 image data rather than a path, so TideSMS writes it to a small cache and shows it against the message; the full-resolution file still comes from **d**.

## Global search

**Ctrl+F**, or **Ctrl+P → Search all messages**, searches every cached message at once. The index is SQLite FTS5 over message bodies, kept in step by triggers, so search stays fast with tens of thousands of messages and never scans the table. Results show the person, date and a snippet.

Filters use the same simple syntax: `from:Amy`, `before:2026-09-01`, `after:2026-08-01`, and quoted phrases such as `"dentist appointment"`. Free text and filters combine.

Pressing **Enter** on a result opens that thread, widens the loaded window from the local cache until the message is present, selects it, and scrolls it into view with a brief highlight — it does not drop you at the newest message. The next navigation key clears the highlight.

## Configuration and storage

Defaults respect XDG directory environment variables:

- `~/.config/tidesms/config.toml`
- `~/.local/share/tidesms/state.db`
- `~/.local/state/tidesms/tidesms.log`

```toml
[sync]
initial_messages = 100 # first batch per thread, 1-1000
page_size = 100        # additional batch size, 1-1000

[contacts]
sync_from_phone = true # import the phone's address book as a read-only overlay

[notifications]
enabled = true
show_sender = true
show_body = true # false announces "New SMS" without the text
sound = false
privacy = false  # true shows the sender only, never the contents

[conversation]
timestamps = "smart"        # or "full"
show_date_separators = true
max_width = 0               # widest a message may grow; 0 uses the pane
bubbles = true              # draw a frame around each message
corners = "round"           # or "square"
fill_bubbles = true         # paint the frame on the theme's raised surface
inline_media = true         # draw downloaded images in the conversation (real graphics, else braille)
incoming_theme = ""         # bubble palette for received messages; empty derives it
outgoing_theme = ""         # bubble palette for sent messages; empty derives it

[general]
theme = "tide"
compact_status = false

[composer]
mode = "normal"     # or "vim"
enter_sends = true  # Enter submits, Shift+Enter newlines; false restores Enter-as-newline

[kdeconnect]
preferred_device = ""

# The AI writer. Local providers need no cloud service. `default_policy` is the
# privacy default for threads with no override: local, cloud or disabled.
[ai]
enabled = false
provider = "disabled"     # ollama, lmstudio, openai, anthropic, deepseek, custom-openai-compatible
endpoint = ""             # e.g. http://127.0.0.1:1234/v1 for LM Studio
model = ""
api_key = ""              # cloud providers only; never logged
default_policy = "local"  # local, cloud or disabled
inline_marks = true

[queue]
max_attempts = 5 # automatic retries before a message is left failed

[scheduler]
enabled = false # allows the optional background sender to run

[logging]
debug_content = false
```

**Ctrl+P → Open settings** shows a single static panel: every option is one row, and **↑↓** moves between rows, **←→** cycles the theme rows with a live preview, and **Enter** toggles or commits the selected row. Nothing opens a submenu. The panel scrolls only when the window is too short to hold it, marking how many rows are above and below. The AI rows — enabled, provider, endpoint, model, API key and default policy — are the only way to set the assistant up from inside the app; the privacy pickers in the palette narrow an assistant that already exists and cannot bring one into being. Endpoint, model and key are typed rather than cycled: **Enter** opens an inline editor, **Enter** saves and **Esc** abandons it. The key is entered masked and is never rendered afterwards, only its length. A configuration that cannot work is named in the panel itself — no provider, a cloud provider under a local-only policy, a missing key or model — rather than surfacing later as a refusal. An enabled provider needs a model, so enabling asks for one instead of writing a config the app would refuse to load on the next start. Themes are TideUI's own palettes — Catppuccin (Mocha, Latte, Frappé, Macchiato), Nord, Dracula, Gruvbox (dark and light), Tokyo Night (and Day), Rosé Pine (and Moon, Dawn), One Dark, Magenta Geode, Coral Sunset, Lavender Fields Forever, VT100 and VT52 — and any of them can be assigned to an individual contact with **t**, or to a thread with **Ctrl+P → Change thread theme**.

Message bubbles can carry their own themes, one per direction, independent of the conversation pane. **Open settings → Incoming bubbles / Outgoing bubbles** sets the global defaults with a live preview; **Ctrl+P → Change incoming/outgoing bubble theme** (contact scope) and **Change thread incoming/outgoing bubble theme** (thread scope) set overrides. A bubble theme resolves **thread → contact → global**, and an empty value derives the surface from the conversation theme exactly as before. A chosen theme supplies its background, foreground and frame colour; if the fill would match the pane or fail the contrast floor, the derived surface is used instead so the text stays legible.

Themes resolve global → contact → thread, and a contact's or thread's theme applies **only inside the conversation view**. The thread list, contact list, status bar and modals always stay on the global theme, so moving between people recolours the conversation and its border without repainting the interface around it. An explicit theme is used whole there — background, foreground and accent — so the conversation pane is painted in that palette while the panes beside it keep the global one.

Every theme picker previews as you move through it: the conversation repaints under the contact and thread pickers, and the whole interface under the global one. Nothing is saved until Enter, and Esc leaves everything as it was. Without one, the conversation keeps the global palette and only its accent is derived from the phone number, so people remain distinguishable without anything shifting; a group with no theme takes a stable accent from its thread id. Accent names used by earlier versions (`tide`, `rose`, `ocean`, `violet`, `amber`, `mint`, `mono`) still work and are applied as an accent over the default palette, so existing configurations and contacts need no change. Contact and thread themes live in SQLite; global settings and preferred device live in TOML.

The palette is context-sensitive: **Search all messages**, **Schedule message**, **Open outgoing queue**, **Send queued messages**, **AI: Review writing** and the AI rewrite actions, **Change AI policy**, **Contact details**, **Mute thread**, **Unmute thread** and **Toggle notification body preview** join the conversation commands (**Search current thread**, **Refresh conversations**, **Change thread theme**, **Mark thread unread**, **Copy phone number**, **Open contact**, **Jump to newest**, **Sync phone contacts**) while conversations are available.

### Background sending

`tidesms-daemon` drains the outgoing queue and releases scheduled messages without the terminal interface running. It shares the same database, backend and messaging packages as the TUI, so delivery rules are not duplicated. Run it with the same `--config`, `--database` and `--log` paths, or install the optional user service (never enabled automatically):

```sh
install -Dm755 tidesms-daemon ~/.local/bin/tidesms-daemon
install -Dm644 contrib/tidesms.service ~/.config/systemd/user/tidesms.service
systemctl --user daemon-reload
systemctl --user enable --now tidesms.service
```

`tidesms-daemon --once` processes the queue a single time and exits, which is useful for a timer or for testing.

SQLite migrations run transactionally on startup. Threads, participants, messages and per-thread sync state live in the same database, indexed on `messages(thread_id, timestamp)`, `messages(device_id, thread_id, backend_id)` and `threads(device_id, last_timestamp)`. The database is the only source the interface renders from, so navigation stays fast while the phone is slow or absent. Milestone 3 adds tables for the outgoing queue, scheduled messages, AI and notification preferences, and contact sources, plus an FTS5 index over message bodies (`message_fts`) wired with triggers so search never scans the message table.

Contact IDs are independent of phone numbers. Drafts are keyed by thread once a thread exists, and by normalized number before that; a draft written against a number is carried into that person's thread the first time it is opened, and only for an unambiguous one-person thread. Changing a contact's number never transfers its old draft to the new number. Deleting a contact retains its draft, recoverable by entering its number again.

Contacts imported from the phone are stored per device in their own table. They are matched to threads by exact normalized number first; failing that, by the trailing ten digits, because phones commonly store a number as `8165550182` while the same SMS address arrives as `+18165550182`. That looser match is used **only when exactly one contact shares those digits**, so two unrelated numbers are never merged on resemblance alone, and an exact match always wins. Numbers are normalized before matching, preserving the raw form for display and never guessing a country code. Two numbers are only treated as one contact when their normalized forms are identical; a sender with no contact remains a first-class thread shown by number. The same trailing-ten identity decides whether a thread is a group, so one person's number written several ways (`+15124100124`, `15124100124`, `5124100124`) counts once and the thread stays replyable.

Draft saves are debounced by 400 ms and guarded by revisions against out-of-order writes. Normal quit flushes every draft and cancels quitting if saving fails. Signal shutdown also attempts a final flush. Abrupt process termination or power loss can lose edits since the last completed save. Config writes use atomic rename; malformed config is preserved and reported inside the UI, with defaults used until the file is fixed and the app restarted.

Logs use structured JSON. Message contents, destinations, CLI arguments, and raw subprocess output are not logged by default. `debug_content = true` explicitly enables outgoing message text in the log. The database contains plaintext contacts and drafts; new state directories/files use owner-only permissions.

## What “submitted” means

Sending uses an argument vector, never a shell:

```text
kdeconnect-cli --device DEVICE --send-sms MESSAGE --destination NUMBER
```

Before submission, TideSMS saves the draft and rechecks reachability and the loaded SMS plugin. An unavailable plugin blocks submission. If the optional `busctl` check cannot run, capability is displayed as **unknown** and a device can be selected manually.

A successful CLI exit clears the unchanged submitted draft and displays **“Submitted … · delivery unverified.”** Failures retain the draft. There is no automatic retry or offline queue.

Outgoing messages appear immediately as `sending`, become `submitted` when the backend accepts them and `failed` when it does not, and a failed message stays selectable for **r** to retry. Nothing is retried automatically. Statuses read back from the phone are only those KDE Connect actually reports for a message type — sent, queued, failed, sending — and no delivery state is invented for the rest, which stay `unknown`.

The current upstream CLI waits for `sendWithoutConversation` but does **not** inspect its D-Bus error reply. The SMS plugin dispatches a request to Android without returning a delivery receipt. Consequently, even with the preflight checks, CLI success cannot prove Android sent the message. Check the phone for the first live test and whenever submission is uncertain. This is a CLI backend limitation, not a delivery guarantee.

Sources checked for this implementation: [KDE CLI](https://github.com/KDE/kdeconnect-kde/blob/master/cli/kdeconnect-cli.cpp), [SMS plugin](https://invent.kde.org/network/kdeconnect-kde/tree/master/plugins/sms), [device capability interface](https://github.com/KDE/kdeconnect-kde/blob/master/core/device.h).

## Architecture and development

- `internal/app`: state transitions, focus, commands, async side effects; rendering in `view.go` and `history_view.go` only consumes state.
- `internal/domain`: transport-independent threads, messages, participants, events, and the message identity rule. No package outside `internal/backend/kdeconnect` knows about D-Bus.
- `internal/syncer`: backend-to-cache synchronization, watermarks and paging, independent of the UI.
- `ui/conversation`: message layout, wrapping, selection, scrolling and match highlighting.
- `ui/composer`: the only Ripple integration, including clipboard and editor intent handling.
- `ui/components`: TideUI contact list, thread list, recipient header, status/notifications, choices and modal surfaces.
- `internal/backend`: `MessagingBackend` and `ConversationBackend` interfaces and request/device types.
- `internal/backend/kdeconnect`: bounded CLI execution for sending, plus the `org.kde.kdeconnect.device.conversations` D-Bus interface for threads, history and signals, and `org.kde.kdeconnect.device.contacts` with the vCard cache for the address book. No KDE Connect network protocol implementation.
- `internal/backend/fake`: deterministic fixtures — devices, threads, history, injected incoming messages, disconnect/reconnect and send failures — so development and tests need no live phone.
- `internal/storage`, `migrations`: SQLite repository and versioned migrations.
- `internal/notifications`: desktop notifications, escaped as plain text.
- `internal/config`, `internal/logging`, `internal/themes`, `internal/contacts`, `internal/keys`: focused support packages.

```sh
make check
python3 scripts/smoke.py # build first; isolated pseudo-terminal + fake CLI, no real SMS
```

The opt-in read-only phone integration test is:

```sh
TIDESMS_TEST_DEVICE=YOUR_DEVICE_ID go test ./internal/backend/kdeconnect -run TestLiveDiscovery -v
```

Tests drive the real model through the fake backend, covering cached-first startup, synchronization and deduplication, live incoming messages, notification policy and per-thread muting, send/fail/retry, thread-scoped drafts, search, thread themes, offline browsing and reconnect, the offline queue and scheduling, AI review and rewrite, and layout bounds at every adaptive width. Attachments and MMS are not implemented; notification action buttons and contact merge review are planned.
