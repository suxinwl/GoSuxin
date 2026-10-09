package app

import "time"

// The limit belongs to a viewer, not to the entire hosted library. Logged-in
// devices share an account's viewer ID; guests keep their browser identity.
const playbackSessionLimit = 4
const playbackSessionLimitMessage = "当前账号或浏览器同时打开的播放页已达上限（4 个），请关闭其他播放页后重试"

func playbackSessionOwner(session *playbackSession) string {
	if session.viewer != nil {
		return session.viewer.id
	}
	if session.accountID != "" {
		return (accountRecord{ID: session.accountID}).viewerID()
	}
	// Legacy signed Emby links have no account identity and share their own
	// quota. They must not consume any browser or account's slots.
	return "emby:legacy"
}

func (app *UIApp) registerPlayback(session *playbackSession) bool {
	app.playbackMu.Lock()
	var expired []*playbackSession
	count := 0
	owner, now := playbackSessionOwner(session), time.Now()
	for id, existing := range app.playbacks {
		if !existing.expires.IsZero() && !now.Before(existing.expires) {
			delete(app.playbacks, id)
			if existing.timer != nil {
				existing.timer.Stop()
			}
			expired = append(expired, existing)
		} else if playbackSessionOwner(existing) == owner {
			count++
		}
	}
	allowed := count < playbackSessionLimit
	if allowed {
		if app.playbacks == nil {
			app.playbacks = make(map[string]*playbackSession)
		}
		session.viewer.retain()
		session.expires = now.Add(playbackIdleTimeout)
		app.playbacks[session.id] = session
		session.timer = time.AfterFunc(playbackIdleTimeout, func() { app.expirePlayback(session.id) })
	}
	app.playbackMu.Unlock()
	// Cancel media work and release viewers only after releasing playbackMu.
	for _, stale := range expired {
		releasePlaybackSession(stale)
	}
	return allowed
}

func releasePlaybackSession(session *playbackSession) {
	if session == nil {
		return
	}
	if session.cancel != nil {
		session.cancel()
	}
	if session.prefetch != nil {
		session.prefetch.cancel()
	}
	session.viewer.release()
}
