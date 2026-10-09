package suxinvideo

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
	"github.com/google/uuid"
)

// Official Jellyfin UserDto/BaseItemDto IDs are Guid, not arbitrary strings.
// Keep the CMS uint32 identity reversible without a mapping table; the typed
// namespace and version/episode fingerprint make every outward ID a UUID.
func jellyfinTypedGUID(kind byte, nativeID int64, discriminator string) string {
	hash := sha256.Sum256([]byte("xiaoqi-jellyfin-v1\x00" + string(kind) + "\x00" + discriminator))
	var id uuid.UUID
	copy(id[:], hash[:16])
	id[0], id[1] = kind, 0x71
	binary.BigEndian.PutUint32(id[2:6], uint32(nativeID))
	id[6] = (id[6] & 0x0f) | 0x50
	id[8] = (id[8] & 0x3f) | 0x80
	return id.String()
}

func jellyfinParseID(raw string) (kind byte, nativeID int64, valid bool) {
	if id, err := uuid.Parse(raw); err == nil && id[1] == 0x71 && id.Version() == 5 && id.Variant() == uuid.RFC4122 {
		kind, nativeID = id[0], int64(binary.BigEndian.Uint32(id[2:6]))
		switch kind {
		case 'u', 'm', 's', 'e', 'n':
			return kind, nativeID, nativeID > 0
		case 'l':
			return kind, nativeID, true
		}
	}
	// Existing bookmarks from earlier adapter builds stay readable; responses
	// always use the new Guid format, including all linked DTO references.
	if strings.EqualFold(raw, "library-all") {
		return 'l', 0, true
	}
	if strings.HasPrefix(strings.ToLower(raw), "library-") {
		n, err := strconv.ParseUint(raw[len("library-"):], 10, 32)
		return 'l', int64(n), err == nil && n > 0
	}
	if len(raw) < 2 {
		return 0, 0, false
	}
	kind = raw[0]
	if kind != 'u' && kind != 'm' && kind != 's' && kind != 'e' && kind != 'n' {
		return 0, 0, false
	}
	number := strings.SplitN(raw[1:], "-", 2)[0]
	n, err := strconv.ParseUint(number, 10, 32)
	return kind, int64(n), err == nil && n > 0
}

func jellyfinIDKind(raw string) byte {
	kind, _, _ := jellyfinParseID(raw)
	return kind
}
func jellyfinUserID(id int64) string    { return jellyfinTypedGUID('u', id, "user") }
func jellyfinLibraryID(id int64) string { return jellyfinTypedGUID('l', id, "library") }
func jellyfinUserMatches(raw string, id int64) bool {
	return jellyfinIDEqual(raw, jellyfinUserID(id)) || raw == fmt.Sprintf("u%d", id)
}
func jellyfinLibraryItem(ctx context.Context, id int64) (row, error) {
	name := "全部影片"
	if id > 0 {
		category, err := one(ctx, "SELECT name FROM sx_type WHERE id=? AND status=1", id)
		if err != nil {
			return nil, err
		}
		if category == nil {
			return nil, appError(404, "媒体库不存在")
		}
		name = gconv.String(category["name"])
	}
	return row{"Id": jellyfinLibraryID(id), "Name": name, "Type": "CollectionFolder", "CollectionType": "mixed", "IsFolder": true, "ServerId": jellyfinServerID}, nil
}
func jellyfinIDEqual(a, b string) bool {
	left, e1 := uuid.Parse(a)
	right, e2 := uuid.Parse(b)
	return e1 == nil && e2 == nil && left == right
}
func jellyfinEpisodeMatches(vodID int64, key, version, requested string) bool {
	if jellyfinIDEqual(jellyfinEpisodeID(vodID, key, version), requested) {
		return true
	}
	hash := sha256.Sum256([]byte(version + "\x00" + key))
	if requested == fmt.Sprintf("e%d-%s", vodID, hex.EncodeToString(hash[:12])) {
		return true
	}
	if version == fmt.Sprintf("vod:%d", vodID) {
		// Old unaliased records used an implicit empty version. This alias is
		// confined to the same film and episode, never another season/version.
		hash = sha256.Sum256([]byte("\x00" + key))
		return requested == fmt.Sprintf("e%d-%s", vodID, hex.EncodeToString(hash[:12]))
	}
	return false
}
func jellyfinSeasonMatches(vodID int64, version, requested string) bool {
	return jellyfinIDEqual(jellyfinSeasonID(vodID, version), requested) || requested == fmt.Sprintf("n%d-%s", vodID, appHash(version)[:16]) || version == fmt.Sprintf("vod:%d", vodID) && requested == fmt.Sprintf("n%d-%s", vodID, appHash("")[:16])
}
