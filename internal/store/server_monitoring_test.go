package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/model"
)

func TestServerMonitoringTargetsAndPacketCounts(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "monitoring.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	server := &model.Server{Name: "monitor", LatencyProbeEnabled: true}
	other := &model.Server{Name: "other", LatencyProbeEnabled: true}
	for _, item := range []*model.Server{server, other} {
		if err := db.CreateServer(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	task := model.LatencyProbeTask{Name: "selected", Method: model.LatencyProbeModeTCP, Address: "1.1.1.1", Port: 443, Enabled: true, IntervalSeconds: 120, ServerIDs: []int64{server.ID}}
	if err := db.SaveLatencyProbeTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	for i := range 25 {
		at := base.Add(time.Duration(i) * time.Minute)
		insertLatencyProbeResult(t, db, server.ID, "public-cloudflare", "public", 0, "", "tcp", "", "", true, 0, 3, 3, at)
		insertLatencyProbeResult(t, db, server.ID, fmt.Sprintf("t%d-0", task.ID), "custom", task.ID, task.Name, "tcp", "", "", true, 40, 3, 2, at)
		insertLatencyProbeResult(t, db, other.ID, "public-cloudflare", "public", 0, "", "tcp", "", "", false, 0, 3, 0, at)
	}
	read := func() map[int64]*model.ServerMonitoringDisplay {
		t.Helper()
		items, err := db.ListServers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.AttachServerMonitoringDisplays(ctx, items); err != nil {
			t.Fatal(err)
		}
		out := map[int64]*model.ServerMonitoringDisplay{}
		for _, item := range items {
			out[item.ID] = item.MonitoringDisplay
		}
		return out
	}
	initial := read()[server.ID]
	if len(initial.Series) != 1 || len(initial.Series[0].Samples) != 20 || !initial.Series[0].Samples[0].CheckedAt.Equal(base.Add(5*time.Minute)) || initial.Series[0].Samples[0].LatencyMS == nil || *initial.Series[0].Samples[0].LatencyMS != 0 || initial.Series[0].Grade.Basis != model.LatencyGradeBasisFixed {
		t.Fatalf("public readings = %#v", initial)
	}
	server.MonitoringTargetTaskID = task.ID
	if err := db.UpdateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	selected := read()
	if selected[server.ID].Name != "selected" || len(selected[server.ID].Series) != 1 {
		t.Fatalf("selected readings = %#v", selected[server.ID])
	}
	series := selected[server.ID].Series[0]
	point := series.Samples[19]
	if series.Label != "selected" || len(series.Samples) != 20 || point.SampleCount != 3 || point.SuccessCount != 2 || point.LatencyMS == nil || *point.LatencyMS != 40 {
		t.Fatalf("selected series = %#v, point=%#v", series, point)
	}
	if failed := selected[other.ID].Series[0].Samples[19]; failed.LatencyMS != nil || failed.SuccessCount != 0 {
		t.Fatal("failed public probe became available")
	}
	task.Enabled = false
	if err := db.SaveLatencyProbeTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	if disabled := read()[server.ID]; disabled.Enabled || len(disabled.Series) != 0 {
		t.Fatalf("disabled task fell back to public: %#v", disabled)
	}
	task.Enabled = true
	task.ServerIDs = []int64{other.ID}
	if err := db.SaveLatencyProbeTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	if unassigned := read()[server.ID]; unassigned.Enabled || len(unassigned.Series) != 0 {
		t.Fatal("unassigned task still displayed")
	}
	if err := db.DeleteLatencyProbeTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if deleted := read()[server.ID]; deleted.Enabled || len(deleted.Series) != 0 {
		t.Fatal("deleted task fell back to public")
	}
}

func TestServerMonitoringMigratesPreviousSchemaAndPersistsOnRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "previous.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	server := &model.Server{Name: "previous", LatencyProbeEnabled: true, LatencyProbeMode: model.LatencyProbeModeICMP, LatencyProbeIntervalSeconds: 300}
	if err := db.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	task := model.LatencyProbeTask{Name: "persisted", Method: model.LatencyProbeModeTCP, Address: "1.1.1.1", Port: 443, Enabled: true, IntervalSeconds: 120, ServerIDs: []int64{server.ID}}
	if err := db.SaveLatencyProbeTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`alter table server_latency_probe_settings drop column monitoring_target_task_id`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err := db.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.MonitoringTargetTaskID != 0 || stored.Name != "previous" || stored.LatencyProbeMode != model.LatencyProbeModeICMP || stored.LatencyProbeIntervalSeconds != 300 {
		t.Fatalf("migration rewrote settings: %#v", stored)
	}
	stored.MonitoringTargetTaskID = task.ID
	if err := db.UpdateServer(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stored, err = db.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.MonitoringTargetTaskID != task.ID {
		t.Fatal("selection lost on restart")
	}
	stored.MonitoringTargetTaskID = 0
	if err := db.UpdateServer(ctx, stored); err != nil {
		t.Fatal(err)
	}
	stored, err = db.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.MonitoringTargetTaskID != 0 {
		t.Fatal("could not reset to public")
	}
}

func TestServerMonitoringSplitsPresetTargetsAndGradesByRegion(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "monitoring-preset.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	server := &model.Server{Name: "us", LatencyProbeEnabled: true, RegionMode: "manual", RegionCode: "US"}
	if err := db.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	task := model.LatencyProbeTask{Name: "广东 · 中国移动", PresetID: "gd-cm", Province: "广东", Carrier: "中国移动", Method: model.LatencyProbeModeTCP, Port: 80, Enabled: true, IntervalSeconds: 60, ServerIDs: []int64{server.ID}}
	if err := db.SaveLatencyProbeTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	server.MonitoringTargetTaskID = task.ID
	if err := db.UpdateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	for i := range 3 {
		at := base.Add(time.Duration(i) * time.Minute)
		insertLatencyProbeResult(t, db, server.ID, fmt.Sprintf("t%d-gd-cm-v6", task.ID), "regional", task.ID, task.Name, "tcp", "广东", "中国移动", true, 160, 5, 5, at)
		insertLatencyProbeResult(t, db, server.ID, fmt.Sprintf("t%d-gd-cm-v4", task.ID), "regional", task.ID, task.Name, "tcp", "广东", "中国移动", true, 150, 5, 2, at)
	}
	items, err := db.ListServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AttachServerMonitoringDisplays(ctx, items); err != nil {
		t.Fatal(err)
	}
	display := items[0].MonitoringDisplay
	if len(display.Series) != 2 {
		t.Fatalf("series = %#v", display.Series)
	}
	v4, v6 := display.Series[0], display.Series[1]
	if v4.Label != "移动" || v4.Family != "ipv4" || v6.Label != "移动v6" || v6.Family != "ipv6" || len(v4.Samples) != 3 {
		t.Fatalf("labels = %#v / %#v", v4, v6)
	}
	if v4.Grade.Basis != model.LatencyGradeBasisRoute || v4.Grade.ExcellentMS < 160 || v4.Grade.ExcellentMS > 175 {
		t.Fatalf("US → Guangdong grade = %#v", v4.Grade)
	}
}
