package store

import (
	"context"
	"time"
)

// TrafficHourBucket is accepted user traffic summed over one UTC hour.
type TrafficHourBucket struct {
	HourStart time.Time
	Upload    int64
	Download  int64
}

// UserPeriodTraffic is one user's usage in their current traffic period.
type UserPeriodTraffic struct {
	UserID     int64  `json:"user_id"`
	Username   string `json:"username"`
	Nickname   string `json:"nickname,omitempty"`
	Upload     int64  `json:"upload_bytes"`
	Download   int64  `json:"download_bytes"`
	LimitBytes int64  `json:"traffic_limit_bytes"`
}

// TrafficHourly sums accepted traffic per UTC hour since the given time. It
// walks idx_traffic_server once per server so the scan stays a range read.
func (s *Store) TrafficHourly(ctx context.Context, since time.Time) ([]TrafficHourBucket, error) {
	rows, err := s.db.QueryContext(ctx, `select substr(created_at,1,13) as hour, coalesce(sum(upload_bytes),0), coalesce(sum(download_bytes),0)
		from traffic_stats
		where server_id in (select id from servers) and created_at >= ?
		group by hour order by hour`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrafficHourBucket
	for rows.Next() {
		var hour string
		var item TrafficHourBucket
		if err := rows.Scan(&hour, &item.Upload, &item.Download); err != nil {
			return nil, err
		}
		start, err := time.Parse("2006-01-02T15", hour)
		if err != nil {
			continue
		}
		item.HourStart = start.UTC()
		out = append(out, item)
	}
	return out, rows.Err()
}

// TopUserPeriodTraffic returns the users with the most traffic in their
// current period, highest first, plus how many users have any traffic.
func (s *Store) TopUserPeriodTraffic(ctx context.Context, limit int) ([]UserPeriodTraffic, int64, error) {
	if limit <= 0 {
		limit = 8
	}
	const latest = `select p.user_id, p.upload_bytes, p.download_bytes, p.traffic_limit_bytes
		from (
			select user_id, upload_bytes, download_bytes, traffic_limit_bytes,
				row_number() over (partition by user_id order by started_at desc) as rn
			from traffic_periods
		) p where p.rn = 1 and p.upload_bytes + p.download_bytes > 0`
	var count int64
	if err := s.db.QueryRowContext(ctx, `select count(*) from (`+latest+`)`).Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `select u.id, u.username, u.nickname, l.upload_bytes, l.download_bytes, l.traffic_limit_bytes
		from (`+latest+`) l join users u on u.id = l.user_id
		order by l.upload_bytes + l.download_bytes desc, u.id
		limit ?`, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []UserPeriodTraffic{}
	for rows.Next() {
		var item UserPeriodTraffic
		if err := rows.Scan(&item.UserID, &item.Username, &item.Nickname, &item.Upload, &item.Download, &item.LimitBytes); err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	return out, count, rows.Err()
}
