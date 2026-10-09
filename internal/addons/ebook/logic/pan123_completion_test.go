package album

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suxinwl/GoSuxin/framework/frame/g"
)

func TestPanUploadWaitsForVerification(t *testing.T) {
	for _, mode := range []string{"verifying", "not-completed", "permanent", "cancel", "deadline", "corrupt", "create-verifying", "detail-verifying"} {
		t.Run(mode, func(t *testing.T) {
			m := &mockPan{files: map[int64]g.Map{}, chunks: map[int64]map[int][]byte{}, corrupt: mode == "corrupt"}
			calls, details := 0, 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var timestamps []time.Time
			var task string
			var completionDeadline time.Time
			client := &panClient{token: "test-token", expires: time.Now().Add(time.Hour)}
			client.http = &http.Client{Transport: panRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/create") && mode == "create-verifying" {
					return mockResponse(200, g.Map{"code": 20103}), nil
				}
				if strings.HasSuffix(r.URL.Path, "/detail") {
					details++
					if mode == "detail-verifying" {
						return mockResponse(200, g.Map{"code": 20103}), nil
					}
				}
				if strings.HasSuffix(r.URL.Path, "/upload_complete") {
					calls++
					timestamps = append(timestamps, time.Now())
					completionDeadline, _ = r.Context().Deadline()
					if remaining := time.Until(completionDeadline); remaining <= 0 || remaining > 2*time.Minute {
						t.Errorf("completion deadline missing or unbounded: %v", remaining)
					}
					if mode == "permanent" {
						return mockResponse(200, g.Map{"code": 20104, "message": "secret-marker", "x-traceID": "fixture-trace"}), nil
					}
					if mode == "cancel" {
						cancel()
						return mockResponse(200, g.Map{"code": 20103}), nil
					}
					if mode == "deadline" {
						<-r.Context().Done()
						return nil, r.Context().Err()
					}
					if mode == "verifying" && calls <= 5 || mode == "corrupt" && calls == 1 || mode == "not-completed" && calls == 1 {
						var body struct {
							Task string `json:"preuploadID"`
						}
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Fatal(err)
						}
						if task != "" && task != body.Task {
							t.Fatal("changed upload task")
						}
						task = body.Task
						if mode == "not-completed" {
							return mockResponse(200, g.Map{"code": 0, "data": g.Map{"completed": false}}), nil
						}
						return mockResponse(200, g.Map{"code": 20103, "message": "文件正在校验中,请间隔1秒后再试"}), nil
					}
				}
				return m.RoundTrip(r)
			})}
			if mode == "deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 350*time.Millisecond)
				defer stop()
			}
			file := filepath.Join(t.TempDir(), "page.jpg")
			if err := os.WriteFile(file, []byte("test-upload-bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			id, err := client.upload(ctx, file, "page.jpg", 0)
			switch mode {
			case "verifying", "not-completed":
				if err != nil || id != 1 {
					t.Fatalf("upload failed: id=%d err=%v", id, err)
				}
				expected := 6
				if mode == "not-completed" {
					expected = 2
				}
				if calls != expected || details != 1 || m.next != 1 || m.slices != 1 {
					t.Fatalf("incorrect/repeated upload: complete=%d detail=%d create=%d slices=%d", calls, details, m.next, m.slices)
				}
				for i := 1; i < len(timestamps); i++ {
					if timestamps[i].Sub(timestamps[i-1]) < 950*time.Millisecond {
						t.Fatal("completion polling is too frequent")
					}
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) || calls != 1 || details != 0 {
					t.Fatalf("cancel ignored: calls=%d details=%d err=%v", calls, details, err)
				}
			case "deadline":
				if !errors.Is(err, context.DeadlineExceeded) || calls != 1 || details != 0 {
					t.Fatalf("deadline ignored: calls=%d details=%d err=%v", calls, details, err)
				}
			case "corrupt":
				if err == nil || !strings.Contains(err.Error(), "MD5") || details != 1 {
					t.Fatalf("checksum check skipped: %v", err)
				}
			default:
				var apiErr *panAPIError
				if !errors.As(err, &apiErr) {
					t.Fatalf("missing structured API error: %v", err)
				}
				if strings.Contains(err.Error(), "secret-marker") {
					t.Fatal("raw API message leaked")
				}
				if mode == "permanent" && (apiErr.code != 20104 || calls != 1 || details != 0 || !strings.Contains(err.Error(), "upload_complete")) {
					t.Fatal("permanent error retried or hidden")
				}
				if mode == "create-verifying" && (calls != 0 || m.slices != 0) {
					t.Fatal("20103 outside completion was retried")
				}
				if mode == "detail-verifying" && (details != 1 || calls != 1) {
					t.Fatal("detail error treated as a completion response")
				}
			}
		})
	}
}

func TestPanCompletionCancellationWhileWaiting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	calls := 0
	client := &panClient{token: "test-token", expires: time.Now().Add(time.Hour), http: &http.Client{Transport: panRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return mockResponse(200, g.Map{"code": 20103}), nil
	})}}
	start := time.Now()
	_, err := client.completeUpload(ctx, "fixture-task")
	if !errors.Is(err, context.DeadlineExceeded) || calls != 1 || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("verification wait did not cancel promptly: %v, calls=%d", err, calls)
	}
}
