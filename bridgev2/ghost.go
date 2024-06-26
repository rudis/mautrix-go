// Copyright (c) 2024 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package bridgev2

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"

	"github.com/rs/zerolog"
	"go.mau.fi/util/exmime"
	"golang.org/x/exp/constraints"
	"golang.org/x/exp/slices"

	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type Ghost struct {
	*database.Ghost
	Bridge *Bridge
	Log    zerolog.Logger
	Intent MatrixAPI
}

func (br *Bridge) loadGhost(ctx context.Context, dbGhost *database.Ghost, queryErr error, id *networkid.UserID) (*Ghost, error) {
	if queryErr != nil {
		return nil, fmt.Errorf("failed to query db: %w", queryErr)
	}
	if dbGhost == nil {
		if id == nil {
			return nil, nil
		}
		dbGhost = &database.Ghost{
			BridgeID: br.ID,
			ID:       *id,
		}
		err := br.DB.Ghost.Insert(ctx, dbGhost)
		if err != nil {
			return nil, fmt.Errorf("failed to insert new ghost: %w", err)
		}
	}
	ghost := &Ghost{
		Ghost:  dbGhost,
		Bridge: br,
		Log:    br.Log.With().Str("ghost_id", string(dbGhost.ID)).Logger(),
		Intent: br.Matrix.GhostIntent(dbGhost.ID),
	}
	br.ghostsByID[ghost.ID] = ghost
	return ghost, nil
}

func (br *Bridge) unlockedGetGhostByID(ctx context.Context, id networkid.UserID, onlyIfExists bool) (*Ghost, error) {
	cached, ok := br.ghostsByID[id]
	if ok {
		return cached, nil
	}
	idPtr := &id
	if onlyIfExists {
		idPtr = nil
	}
	db, err := br.DB.Ghost.GetByID(ctx, id)
	return br.loadGhost(ctx, db, err, idPtr)
}

func (br *Bridge) GetGhostByMXID(ctx context.Context, mxid id.UserID) (*Ghost, error) {
	ghostID, ok := br.Matrix.ParseGhostMXID(mxid)
	if !ok {
		return nil, nil
	}
	return br.GetGhostByID(ctx, ghostID)
}

func (br *Bridge) GetGhostByID(ctx context.Context, id networkid.UserID) (*Ghost, error) {
	br.cacheLock.Lock()
	defer br.cacheLock.Unlock()
	return br.unlockedGetGhostByID(ctx, id, false)
}

type Avatar struct {
	ID     networkid.AvatarID
	Get    func(ctx context.Context) ([]byte, error)
	Remove bool

	// For pre-uploaded avatars, the MXC URI and hash can be provided directly
	MXC  id.ContentURIString
	Hash [32]byte
}

func (a *Avatar) Reupload(ctx context.Context, intent MatrixAPI, currentHash [32]byte) (id.ContentURIString, [32]byte, error) {
	if a.MXC != "" {
		return a.MXC, a.Hash, nil
	}
	data, err := a.Get(ctx)
	if err != nil {
		return "", [32]byte{}, err
	}
	hash := sha256.Sum256(data)
	if hash == currentHash {
		return "", hash, nil
	}
	mime := http.DetectContentType(data)
	fileName := "avatar" + exmime.ExtensionFromMimetype(mime)
	uri, _, err := intent.UploadMedia(ctx, "", data, fileName, mime)
	if err != nil {
		return "", hash, err
	}
	return uri, hash, nil
}

type UserInfo struct {
	Identifiers []string
	Name        *string
	Avatar      *Avatar
	IsBot       *bool

	ExtraUpdates func(context.Context, *Ghost) bool
}

func (ghost *Ghost) UpdateName(ctx context.Context, name string) bool {
	if ghost.Name == name && ghost.NameSet {
		return false
	}
	ghost.Name = name
	ghost.NameSet = false
	err := ghost.Intent.SetDisplayName(ctx, name)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to set display name")
	} else {
		ghost.NameSet = true
	}
	return true
}

func (ghost *Ghost) UpdateAvatar(ctx context.Context, avatar *Avatar) bool {
	if ghost.AvatarID == avatar.ID && ghost.AvatarSet {
		return false
	}
	ghost.AvatarID = avatar.ID
	if !avatar.Remove {
		newMXC, newHash, err := avatar.Reupload(ctx, ghost.Intent, ghost.AvatarHash)
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Msg("Failed to reupload avatar")
			return true
		} else if newHash == ghost.AvatarHash {
			return true
		}
		ghost.AvatarMXC = newMXC
	} else {
		ghost.AvatarMXC = ""
	}
	ghost.AvatarSet = false
	if err := ghost.Intent.SetAvatarURL(ctx, ghost.AvatarMXC); err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to set avatar URL")
	} else {
		ghost.AvatarSet = true
	}
	return true
}

func (ghost *Ghost) UpdateContactInfo(ctx context.Context, identifiers []string, isBot *bool) bool {
	if identifiers != nil {
		slices.Sort(identifiers)
	}
	if ghost.Metadata.ContactInfoSet &&
		(identifiers == nil || slices.Equal(identifiers, ghost.Metadata.Identifiers)) &&
		(isBot == nil || *isBot == ghost.Metadata.IsBot) {
		return false
	}
	if identifiers != nil {
		ghost.Metadata.Identifiers = identifiers
	}
	if isBot != nil {
		ghost.Metadata.IsBot = *isBot
	}
	bridgeName := ghost.Bridge.Network.GetName()
	meta := &event.BeeperProfileExtra{
		RemoteID:     string(ghost.ID),
		Identifiers:  ghost.Metadata.Identifiers,
		Service:      bridgeName.BeeperBridgeType,
		Network:      bridgeName.NetworkID,
		IsBridgeBot:  false,
		IsNetworkBot: ghost.Metadata.IsBot,
	}
	err := ghost.Intent.SetExtraProfileMeta(ctx, meta)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to set extra profile metadata")
	} else {
		ghost.Metadata.ContactInfoSet = true
	}
	return true
}

func (br *Bridge) allowAggressiveUpdateForType(evtType RemoteEventType) bool {
	if !br.Network.GetCapabilities().AggressiveUpdateInfo {
		return false
	}
	switch evtType {
	case RemoteEventUnknown, RemoteEventMessage, RemoteEventEdit, RemoteEventReaction:
		return true
	default:
		return false
	}
}

func (ghost *Ghost) UpdateInfoIfNecessary(ctx context.Context, source *UserLogin, evtType RemoteEventType) {
	if ghost.Name != "" && ghost.NameSet && !ghost.Bridge.allowAggressiveUpdateForType(evtType) {
		return
	}
	info, err := source.Client.GetUserInfo(ctx, ghost)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to get info to update ghost")
	} else if info != nil {
		ghost.UpdateInfo(ctx, info)
	}
}

func MergeExtraUpdaters[T any](funcs ...func(context.Context, T) bool) func(context.Context, T) bool {
	return func(ctx context.Context, obj T) bool {
		update := false
		for _, f := range funcs {
			update = f(ctx, obj) || update
		}
		return update
	}
}

func NumberMetadataUpdater[T *Ghost | *Portal, MetaType constraints.Integer | constraints.Float](key string, value MetaType) func(context.Context, T) bool {
	return simpleMetadataUpdater[T, MetaType](key, value, database.GetNumberFromMap[MetaType])
}

func SimpleMetadataUpdater[T *Ghost | *Portal, MetaType comparable](key string, value MetaType) func(context.Context, T) bool {
	return simpleMetadataUpdater[T, MetaType](key, value, func(m map[string]any, key string) (MetaType, bool) {
		val, ok := m[key].(MetaType)
		return val, ok
	})
}

func simpleMetadataUpdater[T *Ghost | *Portal, MetaType comparable](key string, value MetaType, getter func(map[string]any, string) (MetaType, bool)) func(context.Context, T) bool {
	return func(ctx context.Context, obj T) bool {
		var meta map[string]any
		switch typedObj := any(obj).(type) {
		case *Ghost:
			meta = typedObj.Metadata.Extra
		case *Portal:
			meta = typedObj.Metadata.Extra
		}
		currentVal, ok := getter(meta, key)
		if ok && currentVal == value {
			return false
		}
		meta[key] = value
		return true
	}
}

func (ghost *Ghost) UpdateInfo(ctx context.Context, info *UserInfo) {
	update := false
	if info.Name != nil {
		update = ghost.UpdateName(ctx, *info.Name) || update
	}
	if info.Avatar != nil {
		update = ghost.UpdateAvatar(ctx, info.Avatar) || update
	}
	if info.Identifiers != nil || info.IsBot != nil {
		update = ghost.UpdateContactInfo(ctx, info.Identifiers, info.IsBot) || update
	}
	if info.ExtraUpdates != nil {
		update = info.ExtraUpdates(ctx, ghost) || update
	}
	if update {
		err := ghost.Bridge.DB.Ghost.Update(ctx, ghost.Ghost)
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Msg("Failed to update ghost in database after updating info")
		}
	}
}
