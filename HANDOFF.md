# Handoff

State as of the last commit on `main`. Eight commits ahead of `origin/main`,
none pushed.

## What is here

Two arcs of work, with very different amounts of confidence behind them.

### Inline graphics and attachments — verified against a real terminal

`eecab65` draws message images with the Kitty graphics protocol instead of
braille. The mechanism is **Unicode placeholders**: the image is transmitted
with a virtual placement and drawn by ordinary `U+10EEEE` cells carrying their
row and column as combining marks, with the image id in the foreground colour.
Each cell measures one column, so a pane pads, truncates, frames and scrolls
them exactly as it does letters — which is why a direct placement cannot work
inside TideUI and this can.

Alongside it:

- The full-screen viewer no longer stretches an image to fill the terminal. It
  asks for a box with the image's own proportions, never larger than its own
  pixels, so a small attachment is shown pixel for pixel.
- KDE Connect ships a **100×100 preview** inline with the message list and keeps
  the part behind a separate request. That preview was being stored as the
  attachment's local file and marked available, so the conversation drew the
  thumbnail and a download refused as "already downloaded" — the image that was
  actually sent could never be reached. Preview and part are now separate
  fields, and `v` fetches the part before opening it. Migration 014 moves
  existing rows and was run against a copy of the real database.

This was checked by spawning the app in a terminal, screenshotting with `grim`
and measuring the result. That loop caught two things reasoning alone got
wrong, including one case where reading a screenshot by eye gave the opposite
answer to measuring it.

### Settings, themes and AI — mixed confidence

- `7dfb5c2`, `342bb9d`, `6d35604` — a contact's theme now shows in the sidebar,
  falling back to their bubble palette, and previews live while being chosen.
- `00d5f76`, `0668065`, `bafaf63` — the settings panel can configure AI at all
  (it could not before), offers models the provider actually serves, is grouped
  into categories, and opens with `,` or `Ctrl+O`. The shortcut list was
  reformatted to match.
- `3f16979`, `53daab0` — two rounds of fixing one bug.

**Read this part with suspicion.** See below.

## What went wrong, so it is not repeated

Enabling AI was reported broken, "fixed", reported broken again, and only
actually fixed on the third attempt.

The cause: every test used a stub model server that **always succeeded**. The
failure path — provider unreachable, which is the normal state here because
Ollama is not running — never executed in any test. `TestEnablingAIChoosesA-`
`ModelFromTheProvider` was green for the entire time the flow was unusable.
Passing tests were taken as evidence when the tests encoded the same wrong
assumption as the code.

The real bug, once driven through a real terminal: with no catalogue the reader
lands in an empty model editor; Enter there committed nothing, closed the
editor, and left the cursor on the model row, where the next Enter reopened it.
Enter alternated between two states forever and the switch never moved.

**If you change the settings or AI flow, drive it with `scripts/drive.py`
before believing a test.**

## scripts/drive.py

Runs the built binary under a pty, feeds a `pyte` screen, and prints what the
app actually displays. Needs `pip install pyte`.

```sh
python3 scripts/drive.py                    # start and dump the first screen
python3 scripts/drive.py --keys ",jjj\r"    # send keys, then dump
```

It answers the terminal queries the program blocks on (`OSC 11`, `CSI 6n`) and
runs against a throwaway `HOME` seeded from the real config.

**Override every XDG root, not just `HOME`.** The app resolves its paths through
`XDG_CONFIG_HOME` when that is set, which it is on this machine. An earlier
probe overrode only `HOME` and wrote to the real `~/.config/tidesms/config.toml`
(restored afterwards). `sandbox()` in the script does this correctly.

## Known state and loose ends

- **Ollama is not running**, so the AI model listing always fails here and the
  typed fallback is the path that matters. Config is `provider = "ollama"`,
  `enabled = false`, `model = ""`.
- **Two cached attachment files** in `~/.cache/kdeconnect.daemon/` are not linked
  to any attachment row — every row now has an empty `local_path` after
  migration 014, so the first `v` on those messages refetches from the phone.
- **`api_key` is stored in plain text** in `config.toml`. The panel enters it
  masked and never renders it afterwards, but the file is not protected.
- **A legacy theme name is not addressable in the picker.** Contacts themed with
  the old accent names (`rose`, `tide`, …) still render correctly, but those
  names are absent from `themes.Names`, so `contactThemeIndex` cannot find them
  and the cursor starts at "automatic". Opening the Contact theme row on such a
  contact previews "no override" before anything is pressed. Pre-existing.
- **`tea.Sequence` is effectively untestable** with the test driver — it wraps an
  unexported Bubble Tea message the driver cannot unwrap, so the inner commands
  never run and `m.busy` sticks. It was removed from the AI path in favour of an
  explicit flag; the remaining use in `applyFetchedAttachment` is only
  half-covered (its test asserts the command is non-nil but never runs it).

## Checks

```sh
go build ./... && go vet ./... && go test ./...   # 17 suites
gofmt -l .                                        # expect no output
python3 scripts/drive.py                          # what the app actually shows
```

Nothing is pushed. `git log --oneline origin/main..HEAD` lists the eight
commits; any of them can still be dropped.
