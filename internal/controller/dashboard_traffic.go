package controller

import (
	"context"
	"sync"
	"time"

	"github.com/OboardProject/oboard/internal/store"
)

const (
	dashboardTrafficDays     = 30
	dashboardTrafficTTL      = 5 * time.Minute
	dashboardTrafficTopUsers = 8
)

type dashboardTrafficDay struct {
	Date     string `json:"date"`
	Upload   int64  `json:"upload_bytes"`
	Download int64  `json:"download_bytes"`
}

type dashboardTraffic struct {
	Timezone  string                    `json:"timezone"`
	Days      []dashboardTrafficDay     `json:"days"`
	Users     []store.UserPeriodTraffic `json:"users"`
	UserCount int64                     `json:"user_count"`
}

// dashboardTrafficCache keeps the 30-day hourly scan off every dashboard poll.
// The user ranking is cheap and always read fresh.
type dashboardTrafficCache struct {
	mu       sync.Mutex
	hourly   []store.TrafficHourBucket
	loadedAt time.Time
}

func (s *Server) dashboardTraffic(ctx context.Context) (dashboardTraffic, error) {
	settings := s.runtimeSettings(ctx)
	loc := trafficLocation(settings)
	now := time.Now()
	hourly, err := s.dashboardTrafficHourly(ctx, now)
	if err != nil {
		return dashboardTraffic{}, err
	}
	users, count, err := s.store.TopUserPeriodTraffic(ctx, dashboardTrafficTopUsers)
	if err != nil {
		return dashboardTraffic{}, err
	}
	return dashboardTraffic{
		Timezone:  loc.String(),
		Days:      bucketDashboardTrafficDays(hourly, now, loc, dashboardTrafficDays),
		Users:     users,
		UserCount: count,
	}, nil
}

func (s *Server) dashboardTrafficHourly(ctx context.Context, now time.Time) ([]store.TrafficHourBucket, error) {
	cache := &s.dashboardTrafficCache
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !cache.loadedAt.IsZero() && now.Sub(cache.loadedAt) < dashboardTrafficTTL && now.After(cache.loadedAt) {
		return cache.hourly, nil
	}
	// One extra day covers any timezone offset at the window's first day.
	since := now.UTC().Truncate(time.Hour).Add(-time.Duration(dashboardTrafficDays+1) * 24 * time.Hour)
	hourly, err := s.store.TrafficHourly(ctx, since)
	if err != nil {
		return nil, err
	}
	cache.hourly, cache.loadedAt = hourly, now
	return hourly, nil
}

// bucketDashboardTrafficDays folds UTC hours into the last `days` calendar
// days of loc, oldest first, with empty days kept as zero.
func bucketDashboardTrafficDays(hourly []store.TrafficHourBucket, now time.Time, loc *time.Location, days int) []dashboardTrafficDay {
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	out := make([]dashboardTrafficDay, days)
	index := make(map[string]int, days)
	for i := 0; i < days; i++ {
		date := today.AddDate(0, 0, i-days+1).Format("2006-01-02")
		out[i].Date = date
		index[date] = i
	}
	for _, bucket := range hourly {
		if i, ok := index[bucket.HourStart.In(loc).Format("2006-01-02")]; ok {
			out[i].Upload += bucket.Upload
			out[i].Download += bucket.Download
		}
	}
	return out
}
