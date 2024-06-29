-- v0 -> v4 (compatible with v1+): Latest revision
CREATE TABLE portal (
	bridge_id       TEXT    NOT NULL,
	id              TEXT    NOT NULL,
	receiver        TEXT    NOT NULL,
	mxid            TEXT,

	parent_id       TEXT,
	-- This is not accessed by the bridge, it's only used for the portal parent foreign key.
	-- Parent groups are probably never DMs, so they don't need a receiver.
	parent_receiver TEXT    NOT NULL DEFAULT '',

	relay_bridge_id TEXT,
	relay_login_id  TEXT,

	name            TEXT    NOT NULL,
	topic           TEXT    NOT NULL,
	avatar_id       TEXT    NOT NULL,
	avatar_hash     TEXT    NOT NULL,
	avatar_mxc      TEXT    NOT NULL,
	name_set        BOOLEAN NOT NULL,
	avatar_set      BOOLEAN NOT NULL,
	topic_set       BOOLEAN NOT NULL,
	in_space        BOOLEAN NOT NULL,
	metadata        jsonb   NOT NULL,

	PRIMARY KEY (bridge_id, id, receiver),
	CONSTRAINT portal_parent_fkey FOREIGN KEY (bridge_id, parent_id, parent_receiver)
		-- Deletes aren't allowed to cascade here:
		-- children should be re-parented or cleaned up manually
		REFERENCES portal (bridge_id, id, receiver) ON UPDATE CASCADE,
	CONSTRAINT portal_relay_fkey FOREIGN KEY (relay_bridge_id, relay_login_id)
		REFERENCES user_login (bridge_id, id)
		ON DELETE SET NULL ON UPDATE CASCADE
);

CREATE TABLE ghost (
	bridge_id   TEXT    NOT NULL,
	id          TEXT    NOT NULL,

	name        TEXT    NOT NULL,
	avatar_id   TEXT    NOT NULL,
	avatar_hash TEXT    NOT NULL,
	avatar_mxc  TEXT    NOT NULL,
	name_set    BOOLEAN NOT NULL,
	avatar_set  BOOLEAN NOT NULL,
	metadata    jsonb   NOT NULL,

	PRIMARY KEY (bridge_id, id)
);

CREATE TABLE message (
	-- Messages have an extra rowid to allow a single relates_to column with ON DELETE SET NULL
	-- If the foreign key used (bridge_id, relates_to), then deleting the target column
	-- would try to set bridge_id to null as well.

	-- only: sqlite (line commented)
--	rowid      INTEGER PRIMARY KEY,
	-- only: postgres
	rowid         BIGINT PRIMARY KEY GENERATED BY DEFAULT AS IDENTITY,

	bridge_id     TEXT   NOT NULL,
	id            TEXT   NOT NULL,
	part_id       TEXT   NOT NULL,
	mxid          TEXT   NOT NULL,

	room_id       TEXT   NOT NULL,
	room_receiver TEXT   NOT NULL,
	sender_id     TEXT   NOT NULL,
	timestamp     BIGINT NOT NULL,
	relates_to    BIGINT,
	metadata      jsonb  NOT NULL,

	CONSTRAINT message_relation_fkey FOREIGN KEY (relates_to)
		REFERENCES message (rowid) ON DELETE SET NULL,
	CONSTRAINT message_room_fkey FOREIGN KEY (bridge_id, room_id, room_receiver)
		REFERENCES portal (bridge_id, id, receiver)
		ON DELETE CASCADE ON UPDATE CASCADE,
	CONSTRAINT message_sender_fkey FOREIGN KEY (bridge_id, sender_id)
		REFERENCES ghost (bridge_id, id)
		ON DELETE CASCADE ON UPDATE CASCADE,
	CONSTRAINT message_real_pkey UNIQUE (bridge_id, id, part_id)
);

CREATE TABLE disappearing_message (
	bridge_id    TEXT   NOT NULL,
	mx_room      TEXT   NOT NULL,
	mxid         TEXT   NOT NULL,
	type         TEXT   NOT NULL,
	timer        BIGINT NOT NULL,
	disappear_at BIGINT,

	PRIMARY KEY (bridge_id, mxid)
);

CREATE TABLE reaction (
	bridge_id       TEXT   NOT NULL,
	message_id      TEXT   NOT NULL,
	message_part_id TEXT   NOT NULL,
	sender_id       TEXT   NOT NULL,
	emoji_id        TEXT   NOT NULL,
	room_id         TEXT   NOT NULL,
	room_receiver   TEXT   NOT NULL,
	mxid            TEXT   NOT NULL,

	timestamp       BIGINT NOT NULL,
	metadata        jsonb  NOT NULL,

	PRIMARY KEY (bridge_id, message_id, message_part_id, sender_id, emoji_id),
	CONSTRAINT reaction_room_fkey FOREIGN KEY (bridge_id, room_id, room_receiver)
		REFERENCES portal (bridge_id, id, receiver)
		ON DELETE CASCADE ON UPDATE CASCADE,
	CONSTRAINT reaction_message_fkey FOREIGN KEY (bridge_id, message_id, message_part_id)
		REFERENCES message (bridge_id, id, part_id)
		ON DELETE CASCADE ON UPDATE CASCADE,
	CONSTRAINT reaction_sender_fkey FOREIGN KEY (bridge_id, sender_id)
		REFERENCES ghost (bridge_id, id)
		ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE "user" (
	bridge_id       TEXT NOT NULL,
	mxid            TEXT NOT NULL,

	management_room TEXT,
	access_token    TEXT,

	PRIMARY KEY (bridge_id, mxid)
);

CREATE TABLE user_login (
	bridge_id  TEXT  NOT NULL,
	user_mxid  TEXT  NOT NULL,
	id         TEXT  NOT NULL,
	space_room TEXT,
	metadata   jsonb NOT NULL,

	PRIMARY KEY (bridge_id, id),
	CONSTRAINT user_login_user_fkey FOREIGN KEY (bridge_id, user_mxid)
		REFERENCES "user" (bridge_id, mxid)
		ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE user_portal (
	bridge_id       TEXT    NOT NULL,
	user_mxid       TEXT    NOT NULL,
	login_id        TEXT    NOT NULL,
	portal_id       TEXT    NOT NULL,
	portal_receiver TEXT    NOT NULL,
	in_space        BOOLEAN NOT NULL,
	preferred       BOOLEAN NOT NULL,
	last_read       BIGINT,

	PRIMARY KEY (bridge_id, user_mxid, login_id, portal_id, portal_receiver),
	CONSTRAINT user_portal_user_login_fkey FOREIGN KEY (bridge_id, login_id)
		REFERENCES user_login (bridge_id, id)
		ON DELETE CASCADE ON UPDATE CASCADE,
	CONSTRAINT user_portal_portal_fkey FOREIGN KEY (bridge_id, portal_id, portal_receiver)
		REFERENCES portal (bridge_id, id, receiver)
		ON DELETE CASCADE ON UPDATE CASCADE
);
