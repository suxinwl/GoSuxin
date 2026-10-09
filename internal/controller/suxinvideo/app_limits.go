package suxinvideo

import (
	"net"
	"sync"
	"time"

	"github.com/suxinwl/GoSuxin/framework/net/ghttp"
	"golang.org/x/time/rate"
)

type appLimitEntry struct {
	limiter *rate.Limiter
	seen    time.Time
}

type appRequestLimiter struct {
	mu      sync.Mutex
	entries map[string]*appLimitEntry
	rate    rate.Limit
	burst   int
}

var appAPILimiter = newAppRequestLimiter(20, 80)
var appMediaLimiter = newAppRequestLimiter(250, 512)

func newAppRequestLimiter(perSecond rate.Limit, burst int) *appRequestLimiter {
	return &appRequestLimiter{entries: make(map[string]*appLimitEntry), rate: perSecond, burst: burst}
}

func (l *appRequestLimiter) allow(peer string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.entries[peer]
	if entry == nil {
		if len(l.entries) >= 4096 {
			oldestPeer := ""
			oldestTime := now
			for key, value := range l.entries {
				if now.Sub(value.seen) > 10*time.Minute {
					delete(l.entries, key)
				} else if value.seen.Before(oldestTime) {
					oldestPeer, oldestTime = key, value.seen
				}
			}
			if len(l.entries) >= 4096 {
				delete(l.entries, oldestPeer)
			}
		}
		entry = &appLimitEntry{limiter: rate.NewLimiter(l.rate, l.burst)}
		l.entries[peer] = entry
	}
	entry.seen = now
	return entry.limiter.AllowN(now, 1)
}

func (l *appRequestLimiter) Middleware(r *ghttp.Request) {
	// Do not accept an arbitrary X-Forwarded-For as a new rate-limit identity.
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	if !l.allow(peer, time.Now()) {
		r.Response.Header().Set("Retry-After", "1")
		appWrite(r, nil, appError(429, "请求过于频繁，请稍后重试"))
		r.ExitAll()
		return
	}
	r.Middleware.Next()
}

func appMediaMiddleware(r *ghttp.Request) {
	if err := AppAuthenticateRequest(r); err != nil {
		appWrite(r, nil, err)
		r.ExitAll()
		return
	}
	appMediaLimiter.Middleware(r)
}
