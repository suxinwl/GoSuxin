package mediastream

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestVODManifestDurationAndBounds(t *testing.T) {
	s, err := New(Config{URL: "https://media.example/video.mp4", Key: make([]byte, 16), Duration: 5300 * time.Millisecond, Binary: "ffmpeg", Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	body := string(s.Manifest(func(int) string { return "segment.ts" }))
	if strings.Count(body, "#EXTINF:") != 3 || !strings.Contains(body, "#EXTINF:1.300000") || !strings.HasSuffix(body, "#EXT-X-ENDLIST\n") {
		t.Fatal(body)
	}
	if _, err = s.Segment(context.Background(), 3); err == nil {
		t.Fatal("out-of-range segment accepted")
	}
}

func TestCancelledTranscodeQueueDoesNotLeakSlots(t *testing.T) {
	if err := acquire(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if err := acquire(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := acquire(ctx, true); err == nil {
		t.Fatal("cancelled queue accepted")
	}
	release()
	release()
	slots.Lock()
	active := slots.active
	slots.Unlock()
	if active != 0 {
		t.Fatalf("leaked %d slots", active)
	}
}
