package analysis

import (
	"testing"
	"time"
)

func TestPeriodUsesInclusiveCalendarDays(t *testing.T) {
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, 10, 8, 23, 59, 0, 0, zone)
	for _, days := range []int{7, 30, 90} {
		start, end, err := Period(now, days)
		if err != nil {
			t.Fatal(err)
		}
		if end.Format("2006-01-02 15:04:05") != "2026-10-09 00:00:00" || !start.AddDate(0, 0, days).Equal(end) {
			t.Fatalf("incorrect range: %v %v", start, end)
		}
	}
	if _, _, err := Period(now, 365); err == nil {
		t.Fatal("accepted unlimited range")
	}
}
func TestTrendFillsMissingDaysWithoutMixingMetrics(t *testing.T) {
	start := time.Date(2024, 2, 28, 0, 0, 0, 0, time.UTC)
	result := fillTrend(start, 3, map[string]int64{"2024-02-29": 4}, map[string]int64{"2024-03-01": 2}, map[string]int64{"2024-02-28": 1})
	if len(result) != 3 || result[1].Name != "2024-02-29" || result[2].Name != "2024-03-01" {
		t.Fatalf("missing leap-day bucket: %#v", result)
	}
	if result[0].Operations != 0 || result[0].AlbumVisits != 1 || result[1].Operations != 4 || result[1].Logins != 0 || result[2].Logins != 2 {
		t.Fatalf("incorrect zero filling: %#v", result)
	}
}
