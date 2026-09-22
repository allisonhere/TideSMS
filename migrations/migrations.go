package migrations

import _ "embed"

//go:embed 001_initial.sql
var Initial string

//go:embed 002_conversations.sql
var Conversations string

//go:embed 003_synced_contacts.sql
var SyncedContacts string

//go:embed 004_contact_match.sql
var ContactMatch string

//go:embed 005_group_from_participants.sql
var GroupFromParticipants string

//go:embed 006_queue_and_preferences.sql
var QueueAndPreferences string

//go:embed 007_message_search.sql
var MessageSearch string

//go:embed 008_bubble_themes.sql
var BubbleThemes string

//go:embed 009_queue_offline_wait.sql
var QueueOfflineWait string
