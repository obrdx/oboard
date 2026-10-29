package store

import (
	"context"
	"github.com/OboardProject/oboard/internal/model"
)

func (s *Store) loadExternalOutboundProbes(ctx context.Context, nodes []model.ExternalOutbound) error {
	rows, err := s.db.QueryContext(ctx, `select external_outbound_id,task_id,fingerprint,status,exit_ip,region_code,error,checked_at from external_outbound_probe_results`)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := make(map[int64]*model.ExternalOutbound, len(nodes))
	for i := range nodes {
		byID[nodes[i].ID] = &nodes[i]
	}
	for rows.Next() {
		var id, taskID int64
		var fingerprint, status, ip, region, message, stamp string
		if err := rows.Scan(&id, &taskID, &fingerprint, &status, &ip, &region, &message, &stamp); err != nil {
			return err
		}
		if node := byID[id]; node != nil {
			node.ProbeTaskID = taskID
			node.ProbeFingerprint, node.ProbeStatus, node.ProbeExitIP, node.ProbeRegionCode, node.ProbeError = fingerprint, status, ip, region, message
			at := parseTime(stamp)
			node.ProbeCheckedAt = &at
		}
	}
	return rows.Err()
}

func (s *Store) CompleteExternalOutboundProbe(ctx context.Context, id int64, fingerprint string, taskID int64, status, ip, region, message string) error {
	_, err := s.db.ExecContext(ctx, `update external_outbound_probe_results set status=?,exit_ip=?,region_code=?,error=?,checked_at=? where external_outbound_id=? and fingerprint=? and task_id=?`, status, ip, region, message, now(), id, fingerprint, taskID)
	return err
}

func (s *Store) BeginControllerExternalOutboundProbe(ctx context.Context, id int64, fingerprint string) error {
	_, err := s.db.ExecContext(ctx, `insert into external_outbound_probe_results(external_outbound_id,fingerprint,task_id,status,checked_at) values(?,?,0,'pending',?) on conflict(external_outbound_id) do update set fingerprint=excluded.fingerprint,task_id=0,status='pending',exit_ip='',region_code='',error='',checked_at=excluded.checked_at`, id, fingerprint, now())
	return err
}
