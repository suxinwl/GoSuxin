package suxinvideo

import "testing"

func resumableManualCollectSnapshot(now int64) row {
	return row{"id": 27, "mode": "manual", "status": "running", "active_key": 1, "source_count": 1, "source_id": 14, "cancel_requested": 0, "page": 16, "type_id": 65, "hours": 12, "one_page": 0, "updated_at": now - 30, "started_at": now - 24*60*60}
}

func TestManualCollectResumeCurrentPageAndFilters(t *testing.T) {
	now := int64(1800000000)
	snapshot := resumableManualCollectSnapshot(now)
	params, err := manualCollectResumeParameters(snapshot, now)
	if err != nil || params == nil || params.SourceID != 14 || params.Page != 16 || params.TypeID != 65 || params.Hours != 12 || params.OnePage {
		t.Fatalf("manual progress and filters were lost: params=%+v error=%v", params, err)
	}
	if snapshot["page"] != 16 || snapshot["status"] != "running" {
		t.Fatal("parameter validation changed the stored task snapshot")
	}
	snapshot["page"], snapshot["one_page"] = "0", "1"
	params, err = manualCollectResumeParameters(snapshot, now)
	if err != nil || params == nil || params.Page != 1 || !params.OnePage {
		t.Fatalf("initial page/one-page job was not preserved: params=%+v error=%v", params, err)
	}
}

func TestManualCollectResumeExcludesStoppedHistoricalAndScheduled(t *testing.T) {
	now := int64(1800000000)
	for _, test := range []struct {
		name, key string
		value     any
	}{
		{"historical", "status", "interrupted"},
		{"completed", "status", "complete"},
		{"stopped", "cancel_requested", 1},
		{"scheduled", "mode", "scheduled"},
		{"auto", "mode", "auto"},
		{"multiple_sources", "source_count", 2},
		{"missing_source", "source_id", 0},
		{"inactive", "active_key", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := resumableManualCollectSnapshot(now)
			snapshot[test.key] = test.value
			params, err := manualCollectResumeParameters(snapshot, now)
			if err != nil || params != nil {
				t.Fatalf("ineligible task would resume: params=%+v error=%v", params, err)
			}
		})
	}
	if params, err := manualCollectResumeParameters(nil, now); err != nil || params != nil {
		t.Fatal("missing job was not ignored")
	}
}

func TestManualCollectResumeRejectsInvalidAndStaleProgress(t *testing.T) {
	now := int64(1800000000)
	for _, test := range []struct {
		name, key string
		value     any
	}{
		{"page_limit", "page", 10001},
		{"malformed_page", "page", "not-a-page"},
		{"fractional_page", "page", "16.5"},
		{"negative_type", "type_id", -1},
		{"type_limit", "type_id", int64(1) << 31},
		{"negative_hours", "hours", -1},
		{"hours_limit", "hours", 721},
		{"malformed_hours", "hours", "abc"},
		{"one_page_limit", "one_page", 2},
		{"stale", "updated_at", now - 601},
		{"future", "updated_at", now + 1},
		{"missing_update", "updated_at", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := resumableManualCollectSnapshot(now)
			snapshot[test.key] = test.value
			params, err := manualCollectResumeParameters(snapshot, now)
			if err == nil || params != nil {
				t.Fatalf("invalid task parameters accepted: params=%+v error=%v", params, err)
			}
		})
	}
	snapshot := resumableManualCollectSnapshot(now)
	snapshot["updated_at"] = now - 600
	if params, err := manualCollectResumeParameters(snapshot, now); err != nil || params == nil {
		t.Fatalf("ten-minute boundary should remain resumable: params=%+v error=%v", params, err)
	}
}
