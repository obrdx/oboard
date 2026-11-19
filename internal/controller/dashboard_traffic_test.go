package controller

import (
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/store"
)

func TestBucketDashboardTrafficDaysUsesTrafficTimezone(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC) // 11:00 on 11-20 in UTC+8
	hourly := []store.TrafficHourBucket{
		{HourStart: time.Date(2026, 11, 19, 15, 0, 0, 0, time.UTC), Upload: 1, Download: 10}, // 23:00 on 11-19 local
		{HourStart: time.Date(2026, 11, 19, 16, 0, 0, 0, time.UTC), Upload: 2, Download: 20}, // 00:00 on 11-20 local
		{HourStart: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Upload: 99, Download: 99},  // outside the window
	}
	days := bucketDashboardTrafficDays(hourly, now, loc, 30)
	if len(days) != 30 || days[0].Date != "2026-10-22" || days[29].Date != "2026-11-20" {
		t.Fatalf("window = %s .. %s (%d days)", days[0].Date, days[len(days)-1].Date, len(days))
	}
	if days[28].Upload != 1 || days[28].Download != 10 || days[29].Upload != 2 || days[29].Download != 20 {
		t.Fatalf("local day buckets = %+v %+v", days[28], days[29])
	}
	var total int64
	for _, day := range days {
		total += day.Upload + day.Download
	}
	if total != 33 {
		t.Fatalf("total = %d, want 33 (out-of-window hour must be dropped)", total)
	}
}
