package store

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

const (
	MetricHourlyRetentionDaysSetting = "server_metric_hourly_retention_days"
	MetricDailyRetentionDaysSetting  = "server_metric_daily_retention_days"
	DefaultMetricHourlyRetentionDays = 90
	DefaultMetricDailyRetentionDays  = 365
	MinMetricHourlyRetentionDays     = 7
	MaxMetricHourlyRetentionDays     = 180
	MinMetricDailyRetentionDays      = 30
	MaxMetricDailyRetentionDays      = 730
	metricAggregateRecomputeWindow   = 48 * time.Hour
)

func MetricHourlyRetentionDays(settings map[string]string) int {
	days, err := strconv.Atoi(settings[MetricHourlyRetentionDaysSetting])
	if err != nil || days < MinMetricHourlyRetentionDays || days > MaxMetricHourlyRetentionDays {
		return DefaultMetricHourlyRetentionDays
	}
	return days
}

func MetricDailyRetentionDays(settings map[string]string) int {
	days, err := strconv.Atoi(settings[MetricDailyRetentionDaysSetting])
	if err != nil || days < MinMetricDailyRetentionDays || days > MaxMetricDailyRetentionDays {
		return DefaultMetricDailyRetentionDays
	}
	return days
}

func (s *Store) BuildMetricHourly(ctx context.Context, at time.Time) (int64, error) {
	history, err := s.historyDB()
	if err != nil {
		return 0, err
	}
	at = at.UTC()
	start := at.Add(-metricAggregateRecomputeWindow).Truncate(time.Hour).Format(time.RFC3339Nano)
	end := at.Truncate(time.Hour).Format(time.RFC3339Nano)
	query := `insert into server_metric_hourly(
		server_id,bucket_start,sample_count,resource_sample_count,cpu_avg,cpu_min,cpu_max,
		memory_used_avg,memory_used_min,memory_used_max,memory_total_avg,memory_total_min,memory_total_max,
		disk_used_avg,disk_used_min,disk_used_max,disk_total_avg,disk_total_min,disk_total_max,
		tcp_avg,tcp_min,tcp_max,udp_avg,udp_min,udp_max,process_avg,process_min,process_max,
		network_upload_avg,network_upload_max,network_download_avg,network_download_max,
		connectivity_known_count,connectivity_available_count,connectivity_unknown_count,
		connectivity_latency_avg,connectivity_latency_min,connectivity_latency_max,
		traffic_upload_last,traffic_download_last,last_sampled_at,updated_at)
	select server_id,strftime('%Y-%m-%dT%H:00:00Z',sampled_at),count(*),
		sum(case when resource_recorded=1 then 1 else 0 end),
		coalesce(avg(case when resource_recorded=1 then cpu_usage_percent end),0),
		min(case when resource_recorded=1 then cpu_usage_percent end),
		max(case when resource_recorded=1 then cpu_usage_percent end),
		coalesce(avg(case when resource_recorded=1 then memory_used_bytes end),0),
		min(case when resource_recorded=1 then memory_used_bytes end),
		max(case when resource_recorded=1 then memory_used_bytes end),
		coalesce(avg(case when resource_recorded=1 then memory_total_bytes end),0),
		min(case when resource_recorded=1 then memory_total_bytes end),
		max(case when resource_recorded=1 then memory_total_bytes end),
		coalesce(avg(case when resource_recorded=1 then disk_used_bytes end),0),
		min(case when resource_recorded=1 then disk_used_bytes end),
		max(case when resource_recorded=1 then disk_used_bytes end),
		coalesce(avg(case when resource_recorded=1 then disk_total_bytes end),0),
		min(case when resource_recorded=1 then disk_total_bytes end),
		max(case when resource_recorded=1 then disk_total_bytes end),
		coalesce(avg(case when resource_recorded=1 then tcp_connection_count end),0),
		min(case when resource_recorded=1 then tcp_connection_count end),
		max(case when resource_recorded=1 then tcp_connection_count end),
		coalesce(avg(case when resource_recorded=1 then udp_connection_count end),0),
		min(case when resource_recorded=1 then udp_connection_count end),
		max(case when resource_recorded=1 then udp_connection_count end),
		coalesce(avg(case when resource_recorded=1 then process_count end),0),
		min(case when resource_recorded=1 then process_count end),
		max(case when resource_recorded=1 then process_count end),
		coalesce(avg(network_upload_bps),0),coalesce(max(network_upload_bps),0),
		coalesce(avg(network_download_bps),0),coalesce(max(network_download_bps),0),
		sum(case when connectivity_available>=0 then 1 else 0 end),
		sum(case when connectivity_available=1 then 1 else 0 end),
		sum(case when connectivity_available<0 then 1 else 0 end),
		coalesce(avg(case when connectivity_available=1 then connectivity_latency_ms end),0),
		min(case when connectivity_available=1 then connectivity_latency_ms end),
		max(case when connectivity_available=1 then connectivity_latency_ms end),
		(select traffic_upload_bytes from server_metric_samples s2 where s2.server_id=s1.server_id and strftime('%Y-%m-%dT%H:00:00Z',s2.sampled_at)=strftime('%Y-%m-%dT%H:00:00Z',s1.sampled_at) order by s2.sampled_at desc limit 1),
		(select traffic_download_bytes from server_metric_samples s2 where s2.server_id=s1.server_id and strftime('%Y-%m-%dT%H:00:00Z',s2.sampled_at)=strftime('%Y-%m-%dT%H:00:00Z',s1.sampled_at) order by s2.sampled_at desc limit 1),
		max(sampled_at),?
	from server_metric_samples s1
	where sampled_at>=? and sampled_at<?
	group by server_id,strftime('%Y-%m-%dT%H:00:00Z',sampled_at)
	on conflict(server_id,bucket_start) do update set
		sample_count=excluded.sample_count,resource_sample_count=excluded.resource_sample_count,
		cpu_avg=excluded.cpu_avg,cpu_min=excluded.cpu_min,cpu_max=excluded.cpu_max,
		memory_used_avg=excluded.memory_used_avg,memory_used_min=excluded.memory_used_min,memory_used_max=excluded.memory_used_max,
		memory_total_avg=excluded.memory_total_avg,memory_total_min=excluded.memory_total_min,memory_total_max=excluded.memory_total_max,
		disk_used_avg=excluded.disk_used_avg,disk_used_min=excluded.disk_used_min,disk_used_max=excluded.disk_used_max,
		disk_total_avg=excluded.disk_total_avg,disk_total_min=excluded.disk_total_min,disk_total_max=excluded.disk_total_max,
		tcp_avg=excluded.tcp_avg,tcp_min=excluded.tcp_min,tcp_max=excluded.tcp_max,
		udp_avg=excluded.udp_avg,udp_min=excluded.udp_min,udp_max=excluded.udp_max,
		process_avg=excluded.process_avg,process_min=excluded.process_min,process_max=excluded.process_max,
		network_upload_avg=excluded.network_upload_avg,network_upload_max=excluded.network_upload_max,
		network_download_avg=excluded.network_download_avg,network_download_max=excluded.network_download_max,
		connectivity_known_count=excluded.connectivity_known_count,
		connectivity_available_count=excluded.connectivity_available_count,
		connectivity_unknown_count=excluded.connectivity_unknown_count,
		connectivity_latency_avg=excluded.connectivity_latency_avg,
		connectivity_latency_min=excluded.connectivity_latency_min,
		connectivity_latency_max=excluded.connectivity_latency_max,
		traffic_upload_last=excluded.traffic_upload_last,traffic_download_last=excluded.traffic_download_last,
		last_sampled_at=excluded.last_sampled_at,updated_at=excluded.updated_at`
	result, err := history.ExecContext(ctx, query, now(), start, end)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) BuildMetricDaily(ctx context.Context, at time.Time) (int64, error) {
	history, err := s.historyDB()
	if err != nil {
		return 0, err
	}
	at = at.UTC()
	start := at.Add(-72 * time.Hour).Truncate(24 * time.Hour).Format(time.RFC3339Nano)
	end := at.Truncate(24 * time.Hour).Add(24 * time.Hour).Format(time.RFC3339Nano)
	query := `insert into server_metric_daily(
		server_id,bucket_start,sample_count,resource_sample_count,cpu_avg,cpu_min,cpu_max,
		memory_used_avg,memory_used_min,memory_used_max,memory_total_avg,memory_total_min,memory_total_max,
		disk_used_avg,disk_used_min,disk_used_max,disk_total_avg,disk_total_min,disk_total_max,
		tcp_avg,tcp_min,tcp_max,udp_avg,udp_min,udp_max,process_avg,process_min,process_max,
		network_upload_avg,network_upload_max,network_download_avg,network_download_max,
		connectivity_known_count,connectivity_available_count,connectivity_unknown_count,
		connectivity_latency_avg,connectivity_latency_min,connectivity_latency_max,
		traffic_upload_last,traffic_download_last,last_sampled_at,updated_at)
	select server_id,substr(bucket_start,1,10),sum(sample_count),sum(resource_sample_count),
		case when sum(resource_sample_count)=0 then 0 else sum(cpu_avg*resource_sample_count)/sum(resource_sample_count) end,
		min(cpu_min),max(cpu_max),
		case when sum(resource_sample_count)=0 then 0 else sum(memory_used_avg*resource_sample_count)/sum(resource_sample_count) end,
		min(memory_used_min),max(memory_used_max),
		case when sum(resource_sample_count)=0 then 0 else sum(memory_total_avg*resource_sample_count)/sum(resource_sample_count) end,
		min(memory_total_min),max(memory_total_max),
		case when sum(resource_sample_count)=0 then 0 else sum(disk_used_avg*resource_sample_count)/sum(resource_sample_count) end,
		min(disk_used_min),max(disk_used_max),
		case when sum(resource_sample_count)=0 then 0 else sum(disk_total_avg*resource_sample_count)/sum(resource_sample_count) end,
		min(disk_total_min),max(disk_total_max),
		case when sum(resource_sample_count)=0 then 0 else sum(tcp_avg*resource_sample_count)/sum(resource_sample_count) end,
		min(tcp_min),max(tcp_max),
		case when sum(resource_sample_count)=0 then 0 else sum(udp_avg*resource_sample_count)/sum(resource_sample_count) end,
		min(udp_min),max(udp_max),
		case when sum(resource_sample_count)=0 then 0 else sum(process_avg*resource_sample_count)/sum(resource_sample_count) end,
		min(process_min),max(process_max),
		case when sum(sample_count)=0 then 0 else sum(network_upload_avg*sample_count)/sum(sample_count) end,
		max(network_upload_max),
		case when sum(sample_count)=0 then 0 else sum(network_download_avg*sample_count)/sum(sample_count) end,
		max(network_download_max),
		sum(connectivity_known_count),sum(connectivity_available_count),sum(connectivity_unknown_count),
		case when sum(connectivity_available_count)=0 then 0 else sum(connectivity_latency_avg*connectivity_available_count)/sum(connectivity_available_count) end,
		min(connectivity_latency_min),max(connectivity_latency_max),
		(select traffic_upload_last from server_metric_hourly h2 where h2.server_id=h1.server_id and substr(h2.bucket_start,1,10)=substr(h1.bucket_start,1,10) order by h2.bucket_start desc limit 1),
		(select traffic_download_last from server_metric_hourly h2 where h2.server_id=h1.server_id and substr(h2.bucket_start,1,10)=substr(h1.bucket_start,1,10) order by h2.bucket_start desc limit 1),
		max(last_sampled_at),?
	from server_metric_hourly h1
	where bucket_start>=? and bucket_start<?
	group by server_id,substr(bucket_start,1,10)
	on conflict(server_id,bucket_start) do update set
		sample_count=excluded.sample_count,resource_sample_count=excluded.resource_sample_count,
		cpu_avg=excluded.cpu_avg,cpu_min=excluded.cpu_min,cpu_max=excluded.cpu_max,
		memory_used_avg=excluded.memory_used_avg,memory_used_min=excluded.memory_used_min,memory_used_max=excluded.memory_used_max,
		memory_total_avg=excluded.memory_total_avg,memory_total_min=excluded.memory_total_min,memory_total_max=excluded.memory_total_max,
		disk_used_avg=excluded.disk_used_avg,disk_used_min=excluded.disk_used_min,disk_used_max=excluded.disk_used_max,
		disk_total_avg=excluded.disk_total_avg,disk_total_min=excluded.disk_total_min,disk_total_max=excluded.disk_total_max,
		tcp_avg=excluded.tcp_avg,tcp_min=excluded.tcp_min,tcp_max=excluded.tcp_max,
		udp_avg=excluded.udp_avg,udp_min=excluded.udp_min,udp_max=excluded.udp_max,
		process_avg=excluded.process_avg,process_min=excluded.process_min,process_max=excluded.process_max,
		network_upload_avg=excluded.network_upload_avg,network_upload_max=excluded.network_upload_max,
		network_download_avg=excluded.network_download_avg,network_download_max=excluded.network_download_max,
		connectivity_known_count=excluded.connectivity_known_count,
		connectivity_available_count=excluded.connectivity_available_count,
		connectivity_unknown_count=excluded.connectivity_unknown_count,
		connectivity_latency_avg=excluded.connectivity_latency_avg,
		connectivity_latency_min=excluded.connectivity_latency_min,
		connectivity_latency_max=excluded.connectivity_latency_max,
		traffic_upload_last=excluded.traffic_upload_last,traffic_download_last=excluded.traffic_download_last,
		last_sampled_at=excluded.last_sampled_at,updated_at=excluded.updated_at`
	result, err := history.ExecContext(ctx, query, now(), start, end)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) PurgeMetricHistoryBefore(ctx context.Context, hourlyCutoff, dailyCutoff time.Time) (int64, int64, error) {
	history, err := s.historyDB()
	if err != nil {
		return 0, 0, err
	}
	var hourly, daily int64
	for {
		result, err := history.ExecContext(ctx, `delete from server_metric_hourly where rowid in (
			select rowid from server_metric_hourly where bucket_start<? order by bucket_start limit ?)`, hourlyCutoff.UTC().Format(time.RFC3339Nano), maintenanceBatchSize)
		if err != nil {
			return hourly, daily, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return hourly, daily, err
		}
		hourly += count
		if count < int64(maintenanceBatchSize) {
			break
		}
	}
	for {
		result, err := history.ExecContext(ctx, `delete from server_metric_daily where rowid in (
			select rowid from server_metric_daily where bucket_start<? order by bucket_start limit ?)`, dailyCutoff.UTC().Format(time.RFC3339Nano), maintenanceBatchSize)
		if err != nil {
			return hourly, daily, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return hourly, daily, err
		}
		daily += count
		if count < int64(maintenanceBatchSize) {
			break
		}
	}
	return hourly, daily, nil
}

func (s *Store) HistoryRowCount(ctx context.Context, table string) (int64, error) {
	if !isHistoryTable(table) {
		return 0, sql.ErrNoRows
	}
	history, err := s.historyDB()
	if err != nil {
		return 0, err
	}
	var count int64
	err = history.QueryRowContext(ctx, `select count(*) from `+table).Scan(&count)
	return count, err
}
