-- Full-text index over message bodies. External-content FTS5 keeps the text in
-- the messages table and indexes it here; triggers keep the two in step so no
-- application code has to remember to update the index.
CREATE VIRTUAL TABLE message_fts USING fts5(
 body,
 content='messages',
 content_rowid='rowid',
 tokenize='unicode61 remove_diacritics 2'
);
INSERT INTO message_fts(rowid,body) SELECT rowid,body FROM messages;

CREATE TRIGGER messages_fts_ai AFTER INSERT ON messages BEGIN
 INSERT INTO message_fts(rowid,body) VALUES (new.rowid,new.body);
END;
CREATE TRIGGER messages_fts_ad AFTER DELETE ON messages BEGIN
 INSERT INTO message_fts(message_fts,rowid,body) VALUES ('delete',old.rowid,old.body);
END;
CREATE TRIGGER messages_fts_au AFTER UPDATE ON messages BEGIN
 INSERT INTO message_fts(message_fts,rowid,body) VALUES ('delete',old.rowid,old.body);
 INSERT INTO message_fts(rowid,body) VALUES (new.rowid,new.body);
END;

PRAGMA user_version=7;
