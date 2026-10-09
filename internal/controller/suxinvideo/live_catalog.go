package suxinvideo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/suxinwl/GoSuxin/framework/util/gconv"
)

type LiveGroup struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
type LiveChannel struct {
	ID        int64        `json:"id"`
	TVGID     string       `json:"tvg_id"`
	Name      string       `json:"name"`
	Logo      string       `json:"logo"`
	GroupID   int64        `json:"group_id"`
	GroupName string       `json:"group_name"`
	Enabled   bool         `json:"enabled"`
	Streams   []LiveStream `json:"streams,omitempty"`
}
type LiveStream struct {
	ID             int64             `json:"id"`
	ChannelID      int64             `json:"channel_id"`
	Name           string            `json:"name"`
	URL            string            `json:"-"`
	Headers        map[string]string `json:"-"`
	Priority       int               `json:"priority"`
	Health         string            `json:"health"`
	Quality        string            `json:"quality"`
	Enabled        bool              `json:"enabled"`
	SourceKind     string            `json:"source_kind"`
	ProviderKey    string            `json:"-"`
	ModuleKey      string            `json:"module_key,omitempty"`
	ProviderRef    string            `json:"-"`
	AccessLevel    string            `json:"access_level"`
	SourceRevision int64             `json:"-"`
	MediaType      string            `json:"media_type,omitempty"`
	EpgID          string            `json:"-"`
	Catchup        bool              `json:"catchup"`
	ExpiresAt      int64             `json:"-"`
	SourceMode     string            `json:"mode"`
	EventID        string            `json:"-"`
	DurationMS     int64             `json:"duration_ms,omitempty"`
	ProgrammeID    int64             `json:"programme_id,omitempty"`
}

func liveIdentity(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}
func liveChannelFromRow(r row) LiveChannel {
	return LiveChannel{ID: gconv.Int64(r["id"]), TVGID: gconv.String(r["tvg_id"]), Name: gconv.String(r["name"]), Logo: gconv.String(r["logo"]), GroupID: gconv.Int64(r["group_id"]), GroupName: gconv.String(r["group_name"]), Enabled: liveRowEnabled(r)}
}
func liveStreamFromRow(r row) LiveStream {
	headers := map[string]string{}
	_ = json.Unmarshal([]byte(gconv.String(r["headers_json"])), &headers)
	return LiveStream{ID: gconv.Int64(r["id"]), ChannelID: gconv.Int64(r["channel_id"]), Name: liveStreamDisplayName(r), URL: gconv.String(r["url"]), Headers: headers, Priority: gconv.Int(r["priority"]), Health: gconv.String(r["health"]), Quality: gconv.String(r["quality"]), Enabled: liveRowEnabled(r), SourceKind: gconv.String(r["source_kind"]), ProviderKey: gconv.String(r["provider_key"]), ModuleKey: gconv.String(r["module_key"]), ProviderRef: gconv.String(r["provider_ref"]), AccessLevel: gconv.String(r["access_level"]), SourceRevision: gconv.Int64(r["source_revision"]), MediaType: gconv.String(r["media_type"]), EpgID: gconv.String(r["epg_id"]), Catchup: gconv.Bool(r["catchup_enabled"]), SourceMode: gconv.String(r["source_mode"]), EventID: gconv.String(r["event_id"]), DurationMS: gconv.Int64(r["duration_ms"]), ProgrammeID: gconv.Int64(r["programme_id"])}
}

func liveGroups(ctx context.Context) ([]LiveGroup, error) {
	clause, args, err := liveStreamAccessWhere(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := all(ctx, `SELECT g.id,g.name FROM sx_live_group g WHERE g.enabled=1 AND EXISTS
 (SELECT 1 FROM sx_live_channel c JOIN sx_live_stream s ON s.channel_id=c.id
 WHERE c.group_id=g.id AND c.enabled=1 AND s.enabled=1 AND s.last_checked>0 AND `+clause+`) ORDER BY g.sort,g.id`, args...)
	result := make([]LiveGroup, 0, len(rows))
	for _, r := range rows {
		result = append(result, LiveGroup{gconv.Int64(r["id"]), gconv.String(r["name"])})
	}
	return result, err
}
func liveChannels(ctx context.Context, groupID int64, q string, page, size int) ([]LiveChannel, int, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 30
	}
	if size > 100 {
		size = 100
	}
	clause, args, err := liveStreamAccessWhere(ctx)
	if err != nil {
		return nil, 0, err
	}
	where := ` WHERE c.enabled=1 AND g.enabled=1 AND EXISTS (SELECT 1 FROM sx_live_stream s WHERE s.channel_id=c.id AND s.enabled=1 AND s.last_checked>0 AND ` + clause + `)`
	if groupID > 0 {
		where += " AND c.group_id=?"
		args = append(args, groupID)
	}
	if q = strings.TrimSpace(q); q != "" {
		where += " AND (c.name LIKE ? OR c.tvg_id LIKE ?)"
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	n, err := one(ctx, "SELECT COUNT(*) n FROM sx_live_channel c JOIN sx_live_group g ON g.id=c.group_id"+where, args...)
	if err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]any{}, args...), size, (page-1)*size)
	rows, err := all(ctx, "SELECT c.*,g.name group_name FROM sx_live_channel c JOIN sx_live_group g ON g.id=c.group_id"+where+" ORDER BY c.sort,c.id LIMIT ? OFFSET ?", queryArgs...)
	result := make([]LiveChannel, 0, len(rows))
	for _, r := range rows {
		result = append(result, liveChannelFromRow(r))
	}
	return result, gconv.Int(n["n"]), err
}
func liveChannel(ctx context.Context, id int64) (LiveChannel, error) {
	r, err := one(ctx, "SELECT c.*,g.name group_name,g.enabled group_enabled FROM sx_live_channel c JOIN sx_live_group g ON g.id=c.group_id WHERE c.id=?", id)
	if err != nil {
		return LiveChannel{}, err
	}
	if r == nil {
		return LiveChannel{}, errors.New("直播频道不存在或已停用")
	}
	channel := liveChannelFromRow(r)
	channel.Enabled = channel.Enabled && gconv.Bool(r["group_enabled"])
	channel.Streams, err = liveStreams(ctx, id)
	return channel, err
}
func liveStreams(ctx context.Context, channelID int64) ([]LiveStream, error) {
	clause, aclArgs, err := liveStreamAccessWhere(ctx)
	if err != nil {
		return nil, err
	}
	args := append([]any{channelID}, aclArgs...)
	rows, err := all(ctx, `SELECT s.*,(SELECT COUNT(*) FROM sx_live_stream ranked WHERE ranked.channel_id=s.channel_id AND ranked.id<=s.id) line_no FROM sx_live_stream s JOIN sx_live_channel c ON c.id=s.channel_id JOIN sx_live_group g ON g.id=c.group_id
 WHERE s.channel_id=? AND s.enabled=1 AND c.enabled=1 AND g.enabled=1 AND s.last_checked>0 AND `+clause+` ORDER BY (s.health='healthy') DESC,s.priority DESC,s.id`, args...)
	result := make([]LiveStream, 0, len(rows))
	for _, r := range rows {
		result = append(result, liveStreamFromRow(r))
	}
	return result, err
}
func liveStream(ctx context.Context, id int64) (LiveStream, error) {
	r, err := one(ctx, "SELECT s.*,(SELECT COUNT(*) FROM sx_live_stream ranked WHERE ranked.channel_id=s.channel_id AND ranked.id<=s.id) line_no FROM sx_live_stream s WHERE s.id=?", id)
	if err != nil {
		return LiveStream{}, err
	}
	if r == nil {
		return LiveStream{}, errors.New("直播源不存在")
	}
	return liveStreamFromRow(r), nil
}
func liveSetHealth(ctx context.Context, id int64, healthy bool, summary string) error {
	health := "unavailable"
	if healthy {
		health = "healthy"
		if summary != "" {
			if err := execSQL(ctx, "UPDATE sx_live_stream SET quality=IF(manual_edited=0,?,quality) WHERE id=?", cutRunes(summary, 100), id); err != nil {
				return err
			}
		}
		summary = ""
	}
	return execSQL(ctx, "UPDATE sx_live_stream SET health=?,last_checked=?,last_error=? WHERE id=?", health, time.Now().Unix(), cutRunes(summary, 300), id)
}
