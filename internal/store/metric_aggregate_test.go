package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/model"
)

func TestMetricHourlyAndDailyAggregatesPreserveSemantics(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "metrics.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	server := &model.Server{Name: "metric-rollup", ListenIP: "0.0.0.0", Status: model.ServerOnline}
	if err := s.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	insert := func(at time.Time, cpu float64, upload int64, available int, recorded int) {
		t.Helper()
		if _, err := s.history.ExecContext(ctx, `insert into server_metric_samples(server_id,cpu_usage_percent,memory_used_bytes,memory_total_bytes,disk_used_bytes,disk_total_bytes,tcp_connection_count,udp_connection_count,process_count,resource_recorded,network_upload_bps,network_download_bps,traffic_upload_bytes,traffic_download_bytes,connectivity_available,connectivity_latency_ms,sampled_at)
			values(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, server.ID, cpu, 100, 1000, 200, 2000, 10, 5, 3, recorded, 100, 200, upload, upload*2, available, 25, at.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	insert(hour.Add(1*time.Minute), 10, 100, 1, 1)
	insert(hour.Add(2*time.Minute), 20, 200, 0, 1)
	insert(hour.Add(3*time.Minute), 30, 150, -1, 1)
	if _, err := s.BuildMetricHourly(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var samples, resources int
	var cpuAvg, cpuMin, cpuMax float64
	var known, available, unknown int
	var uploadLast, downloadLast int64
	if err := s.history.QueryRowContext(ctx, `select sample_count,resource_sample_count,cpu_avg,cpu_min,cpu_max,connectivity_known_count,connectivity_available_count,connectivity_unknown_count,traffic_upload_last,traffic_download_last
		from server_metric_hourly where server_id=? and bucket_start=?`, server.ID, hour.Format(time.RFC3339Nano)).Scan(
		&samples, &resources, &cpuAvg, &cpuMin, &cpuMax, &known, &available, &unknown, &uploadLast, &downloadLast); err != nil {
		t.Fatal(err)
	}
	if samples != 3 || resources != 3 || cpuAvg != 20 || cpuMin != 10 || cpuMax != 30 || known != 2 || available != 1 || unknown != 1 {
		t.Fatalf("hourly aggregate = samples:%d resources:%d cpu:%v/%v/%v connectivity:%d/%d/%d", samples, resources, cpuAvg, cpuMin, cpuMax, known, available, unknown)
	}
	if uploadLast != 150 || downloadLast != 300 {
		t.Fatalf("cumulative counters were summed instead of last-value: %d/%d", uploadLast, downloadLast)
	}

	// A late sample must update the existing bucket rather than create a new one.
	insert(hour.Add(4*time.Minute), 40, 250, 1, 1)
	if _, err := s.BuildMetricHourly(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := s.history.QueryRowContext(ctx, `select sample_count,cpu_avg,cpu_max from server_metric_hourly where server_id=? and bucket_start=?`, server.ID, hour.Format(time.RFC3339Nano)).Scan(&samples, &cpuAvg, &cpuMax); err != nil {
		t.Fatal(err)
	}
	if samples != 4 || cpuAvg != 25 || cpuMax != 40 {
		t.Fatalf("late sample aggregate = samples:%d avg:%v max:%v", samples, cpuAvg, cpuMax)
	}
	if _, err := s.BuildMetricDaily(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var dailySamples, dailyResources int
	var dailyAvg float64
	if err := s.history.QueryRowContext(ctx, `select sample_count,resource_sample_count,cpu_avg from server_metric_daily where server_id=? and bucket_start=?`, server.ID, hour.Format("2006-01-02")).Scan(&dailySamples, &dailyResources, &dailyAvg); err != nil {
		t.Fatal(err)
	}
	if dailySamples != 4 || dailyResources != 4 || dailyAvg != 25 {
		t.Fatalf("daily aggregate = samples:%d resources:%d avg:%v", dailySamples, dailyResources, dailyAvg)
	}
}
