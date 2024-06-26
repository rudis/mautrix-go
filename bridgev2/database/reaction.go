// Copyright (c) 2024 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package database

import (
	"context"
	"time"

	"go.mau.fi/util/dbutil"

	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"
)

type ReactionQuery struct {
	BridgeID networkid.BridgeID
	*dbutil.QueryHelper[*Reaction]
}

type StandardReactionMetadata struct {
	Emoji string `json:"emoji,omitempty"`
}

type ReactionMetadata struct {
	StandardReactionMetadata
	Extra map[string]any
}

func (rm *ReactionMetadata) UnmarshalJSON(data []byte) error {
	return unmarshalMerge(data, &rm.StandardReactionMetadata, &rm.Extra)
}

func (rm *ReactionMetadata) MarshalJSON() ([]byte, error) {
	return marshalMerge(&rm.StandardReactionMetadata, rm.Extra)
}

type Reaction struct {
	BridgeID      networkid.BridgeID
	Room          networkid.PortalKey
	MessageID     networkid.MessageID
	MessagePartID networkid.PartID
	SenderID      networkid.UserID
	EmojiID       networkid.EmojiID
	MXID          id.EventID

	Timestamp time.Time
	Metadata  ReactionMetadata
}

func newReaction(_ *dbutil.QueryHelper[*Reaction]) *Reaction {
	return &Reaction{}
}

const (
	getReactionBaseQuery = `
		SELECT bridge_id, message_id, message_part_id, sender_id, emoji_id, room_id, room_receiver, mxid, timestamp, metadata FROM reaction
	`
	getReactionByIDQuery                   = getReactionBaseQuery + `WHERE bridge_id=$1 AND message_id=$2 AND message_part_id=$3 AND sender_id=$4 AND emoji_id=$5`
	getReactionByIDWithoutMessagePartQuery = getReactionBaseQuery + `WHERE bridge_id=$1 AND message_id=$2 AND sender_id=$3 AND emoji_id=$4 ORDER BY message_part_id ASC LIMIT 1`
	getAllReactionsToMessageBySenderQuery  = getReactionBaseQuery + `WHERE bridge_id=$1 AND message_id=$2 AND sender_id=$3 ORDER BY timestamp DESC`
	getAllReactionsToMessageQuery          = getReactionBaseQuery + `WHERE bridge_id=$1 AND message_id=$2`
	getReactionByMXIDQuery                 = getReactionBaseQuery + `WHERE bridge_id=$1 AND mxid=$2`
	upsertReactionQuery                    = `
		INSERT INTO reaction (bridge_id, message_id, message_part_id, sender_id, emoji_id, room_id, room_receiver, mxid, timestamp, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (bridge_id, room_receiver, message_id, message_part_id, sender_id, emoji_id)
		DO UPDATE SET mxid=excluded.mxid, timestamp=excluded.timestamp, metadata=excluded.metadata
	`
	deleteReactionQuery = `
		DELETE FROM reaction WHERE bridge_id=$1 AND message_id=$2 AND message_part_id=$3 AND sender_id=$4 AND emoji_id=$5
	`
)

func (rq *ReactionQuery) GetByID(ctx context.Context, messageID networkid.MessageID, messagePartID networkid.PartID, senderID networkid.UserID, emojiID networkid.EmojiID) (*Reaction, error) {
	return rq.QueryOne(ctx, getReactionByIDQuery, rq.BridgeID, messageID, messagePartID, senderID, emojiID)
}

func (rq *ReactionQuery) GetByIDWithoutMessagePart(ctx context.Context, messageID networkid.MessageID, senderID networkid.UserID, emojiID networkid.EmojiID) (*Reaction, error) {
	return rq.QueryOne(ctx, getReactionByIDWithoutMessagePartQuery, rq.BridgeID, messageID, senderID, emojiID)
}

func (rq *ReactionQuery) GetAllToMessageBySender(ctx context.Context, messageID networkid.MessageID, senderID networkid.UserID) ([]*Reaction, error) {
	return rq.QueryMany(ctx, getAllReactionsToMessageBySenderQuery, rq.BridgeID, messageID, senderID)
}

func (rq *ReactionQuery) GetAllToMessage(ctx context.Context, messageID networkid.MessageID) ([]*Reaction, error) {
	return rq.QueryMany(ctx, getAllReactionsToMessageQuery, rq.BridgeID, messageID)
}

func (rq *ReactionQuery) GetByMXID(ctx context.Context, mxid id.EventID) (*Reaction, error) {
	return rq.QueryOne(ctx, getReactionByMXIDQuery, rq.BridgeID, mxid)
}

func (rq *ReactionQuery) Upsert(ctx context.Context, reaction *Reaction) error {
	ensureBridgeIDMatches(&reaction.BridgeID, rq.BridgeID)
	return rq.Exec(ctx, upsertReactionQuery, reaction.sqlVariables()...)
}

func (rq *ReactionQuery) Delete(ctx context.Context, reaction *Reaction) error {
	ensureBridgeIDMatches(&reaction.BridgeID, rq.BridgeID)
	return rq.Exec(ctx, deleteReactionQuery, reaction.BridgeID, reaction.MessageID, reaction.MessagePartID, reaction.SenderID, reaction.EmojiID)
}

func (r *Reaction) Scan(row dbutil.Scannable) (*Reaction, error) {
	var timestamp int64
	err := row.Scan(
		&r.BridgeID, &r.MessageID, &r.MessagePartID, &r.SenderID, &r.EmojiID,
		&r.Room.ID, &r.Room.Receiver, &r.MXID, &timestamp, dbutil.JSON{Data: &r.Metadata},
	)
	if err != nil {
		return nil, err
	}
	if r.Metadata.Extra == nil {
		r.Metadata.Extra = make(map[string]any)
	}
	r.Timestamp = time.Unix(0, timestamp)
	return r, nil
}

func (r *Reaction) sqlVariables() []any {
	if r.Metadata.Extra == nil {
		r.Metadata.Extra = make(map[string]any)
	}
	return []any{
		r.BridgeID, r.MessageID, r.MessagePartID, r.SenderID, r.EmojiID,
		r.Room.ID, r.Room.Receiver, r.MXID, r.Timestamp.UnixNano(), dbutil.JSON{Data: &r.Metadata},
	}
}
