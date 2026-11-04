package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
)

// AttachServerMonitoringDisplays reads the selected target's latest 20 probe
// rounds. It is separate from runtime server reads and never changes SLA state.
func (s *Store) AttachServerMonitoringDisplays(ctx context.Context, servers []model.Server) error {
	if len(servers) == 0 {
		return nil
	}
	ids, placeholders := serverIDQueryArgs(servers)
	byID := make(map[int64]*model.Server, len(servers))
	for i := range servers {
		server := &servers[i]
		server.MonitoringDisplay = &model.ServerMonitoringDisplay{Name: "公网探测", Enabled: server.LatencyProbeEnabled, Series: []model.ServerMonitoringSeries{}}
		if server.MonitoringTargetTaskID != 0 {
			server.MonitoringDisplay.Name = "探测任务不可用"
			server.MonitoringDisplay.Enabled = false
		}
		byID[server.ID] = server
	}
	rows, err := s.db.QueryContext(ctx, `select m.server_id,t.id,t.name from latency_probe_task_servers m join latency_probe_tasks t on t.id=m.task_id where t.enabled=1 and m.server_id in (`+placeholders+`)`, ids...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var serverID, taskID int64
		var name string
		if err := rows.Scan(&serverID, &taskID, &name); err != nil {
			rows.Close()
			return err
		}
		server := byID[serverID]
		if server.MonitoringTargetTaskID == taskID {
			server.MonitoringDisplay.Name = name
			server.MonitoringDisplay.Enabled = server.LatencyProbeEnabled
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	values := []string{}
	args := []any{}
	for _, server := range servers {
		if !server.MonitoringDisplay.Enabled {
			continue
		}
		values = append(values, "(?,?)")
		args = append(args, server.ID, server.MonitoringTargetTaskID)
	}
	if len(values) == 0 {
		return nil
	}
	// Each target of the selected task is its own series (for example the IPv4
	// and the IPv6 address of a carrier preset), bounded to the server's latest
	// 20 rounds of that task.
	history, err := s.historyDB()
	if err != nil {
		return err
	}
	rows, err = history.QueryContext(ctx, `with targets(server_id,task_id) as (values `+strings.Join(values, ",")+`)
	select t.server_id,r.probe_id,max(r.kind),max(r.province),max(r.carrier),r.checked_at,max(r.available),
	  sum(case when r.available=1 then r.latency_ms * max(1,r.success_count) else 0 end) * 1.0 / nullif(sum(case when r.available=1 then max(1,r.success_count) else 0 end),0),
	  sum(max(1,r.sample_count,r.success_count)),
	  sum(case when r.sample_count=0 and r.success_count=0 then r.available else min(max(0,r.success_count),max(1,r.sample_count,r.success_count)) end)
	from targets t join server_latency_probe_results r on r.server_id=t.server_id
	where ((t.task_id=0 and r.kind='public') or (t.task_id>0 and r.kind<>'public' and r.task_id=t.task_id))
	and r.checked_at in (
	  select distinct p.checked_at from server_latency_probe_results p
	  where p.server_id=t.server_id and ((t.task_id=0 and p.kind='public') or (t.task_id>0 and p.kind<>'public' and p.task_id=t.task_id))
	  order by p.checked_at desc limit 20
	)
	group by t.server_id,r.probe_id,r.checked_at order by t.server_id,r.probe_id,r.checked_at`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	type seriesKey struct {
		serverID int64
		probeID  string
	}
	index := map[seriesKey]int{}
	for rows.Next() {
		var serverID int64
		var probeID, kind, province, carrier, checked string
		var available int
		var latency sql.NullFloat64
		var sample model.ServerMonitoringSample
		if err := rows.Scan(&serverID, &probeID, &kind, &province, &carrier, &checked, &available, &latency, &sample.SampleCount, &sample.SuccessCount); err != nil {
			return err
		}
		sample.CheckedAt, err = time.Parse(time.RFC3339Nano, checked)
		if err != nil {
			return err
		}
		sample.Available = available != 0
		if latency.Valid {
			sample.LatencyMS = &latency.Float64
		}
		server := byID[serverID]
		key := seriesKey{serverID, probeID}
		position, ok := index[key]
		if !ok {
			if len(server.MonitoringDisplay.Series) >= maxMonitoringSeries {
				continue
			}
			position = len(server.MonitoringDisplay.Series)
			index[key] = position
			server.MonitoringDisplay.Series = append(server.MonitoringDisplay.Series, monitoringSeries(*server, probeID, kind, province, carrier))
		}
		series := &server.MonitoringDisplay.Series[position]
		series.Samples = append(series.Samples, sample)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range servers {
		labelMonitoringSeries(servers[i].MonitoringDisplay)
	}
	return nil
}

const maxMonitoringSeries = 4

// monitoringSeries names one target and picks the latency scale it is judged
// on. A carrier preset target is graded for the route from this server's
// region to the target province; public and custom targets have no known
// location and use the fixed scale.
func monitoringSeries(server model.Server, probeID, kind, province, carrier string) model.ServerMonitoringSeries {
	series := model.ServerMonitoringSeries{ProbeID: probeID, Grade: core.FixedLatencyGrade, Samples: []model.ServerMonitoringSample{}}
	switch kind {
	case "regional":
		series.Label = core.LatencyCarrierShortName(carrier)
		series.Family = core.LatencyProbeTargetFamily(probeID)
		if series.Family == "ipv6" {
			series.Label += "v6"
		}
		region, _ := core.EffectiveServerRegion(server)
		series.Grade, _ = core.RouteLatencyGrade(region, province)
	default:
		series.Label = server.MonitoringDisplay.Name
	}
	if series.Label == "" {
		series.Label = server.MonitoringDisplay.Name
	}
	return series
}

// labelMonitoringSeries keeps labels distinct when one task has several
// targets of the same carrier and family (for example two IPv4 addresses).
func labelMonitoringSeries(display *model.ServerMonitoringDisplay) {
	if display == nil {
		return
	}
	seen := map[string]int{}
	for i := range display.Series {
		label := display.Series[i].Label
		seen[label]++
		if seen[label] > 1 {
			display.Series[i].Label = label + " " + strconv.Itoa(seen[label])
		}
	}
}
