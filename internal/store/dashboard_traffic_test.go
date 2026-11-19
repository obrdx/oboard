package store

import (
	"context"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/model"
)

func TestDashboardTrafficQueries(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	ts := "2026-11-20T00:00:00Z"
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	server := &model.Server{Name: "n1", ListenIP: "0.0.0.0"}
	if err := s.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	for id, name := range map[int64]string{1: "alice", 2: "bob", 3: "idle"} {
		exec(`insert into users(id,username,password_hash,role,status,proxy_uuid,proxy_password,created_at,updated_at) values(?,?,'x','user','active',?,'p',?,?)`, id, name, name, ts, ts)
	}
	exec(`insert into traffic_periods(user_id,period_key,started_at,ends_at,upload_bytes,download_bytes,updated_at) values(1,'old','2026-10-01T00:00:00Z','2026-11-01T00:00:00Z',900,900,?)`, ts)
	exec(`insert into traffic_periods(user_id,period_key,started_at,ends_at,upload_bytes,download_bytes,updated_at) values(1,'cur','2026-11-01T00:00:00Z','2026-12-01T00:00:00Z',10,90,?)`, ts)
	exec(`insert into traffic_periods(user_id,period_key,started_at,ends_at,upload_bytes,download_bytes,updated_at) values(2,'cur','2026-11-01T00:00:00Z','2026-12-01T00:00:00Z',20,180,?)`, ts)
	exec(`insert into traffic_periods(user_id,period_key,started_at,ends_at,upload_bytes,download_bytes,updated_at) values(3,'cur','2026-11-01T00:00:00Z','2026-12-01T00:00:00Z',0,0,?)`, ts)
	users, count, err := s.TopUserPeriodTraffic(ctx, 8)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || len(users) != 2 || users[0].Username != "bob" || users[1].Username != "alice" || users[1].Download != 90 {
		t.Fatalf("ranking = %+v count=%d", users, count)
	}

	exec(`insert into traffic_stats(server_id,user_id,upload_bytes,download_bytes,created_at) values(?,1,1,2,'2026-11-19T08:10:00.5Z')`, server.ID)
	exec(`insert into traffic_stats(server_id,user_id,upload_bytes,download_bytes,created_at) values(?,2,3,4,'2026-11-19T08:59:59Z')`, server.ID)
	exec(`insert into traffic_stats(server_id,user_id,upload_bytes,download_bytes,created_at) values(?,2,5,6,'2026-11-19T09:00:00Z')`, server.ID)
	exec(`insert into traffic_stats(server_id,user_id,upload_bytes,download_bytes,created_at) values(?,2,7,8,'2026-10-01T00:00:00Z')`, server.ID)
	hourly, err := s.TrafficHourly(ctx, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(hourly) != 2 || !hourly[0].HourStart.Equal(time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)) || hourly[0].Upload != 4 || hourly[0].Download != 6 || hourly[1].Upload != 5 {
		t.Fatalf("hourly = %+v", hourly)
	}
}
