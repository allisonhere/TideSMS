-- Per-contact and per-thread bubble themes. The existing theme column colours
-- the conversation pane; these two colour the message surfaces themselves, one
-- for each direction. An empty value keeps the derived surface, so nothing
-- changes until a bubble theme is chosen.
ALTER TABLE contacts ADD COLUMN theme_in TEXT NOT NULL DEFAULT '';
ALTER TABLE contacts ADD COLUMN theme_out TEXT NOT NULL DEFAULT '';
ALTER TABLE threads ADD COLUMN theme_in TEXT NOT NULL DEFAULT '';
ALTER TABLE threads ADD COLUMN theme_out TEXT NOT NULL DEFAULT '';

PRAGMA user_version=8;
