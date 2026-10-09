package suxinvideo

import (
	"testing"
	"time"
)

func TestAppMediaLimitIndependentFromAPI(t *testing.T) {
	api := newAppRequestLimiter(20, 2)
	media := newAppRequestLimiter(250, 5)
	now := time.Now()
	if !api.allow("one", now) || !api.allow("one", now) || api.allow("one", now) {
		t.Fatal("API burst budget was not enforced")
	}
	if !api.allow("two", now) || !media.allow("one", now) {
		t.Fatal("another peer or media inherited the exhausted API budget")
	}
}
