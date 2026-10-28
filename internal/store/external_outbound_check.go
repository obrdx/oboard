package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/OboardProject/oboard/internal/model"
)

// ErrNetworkDiagnosticBusy reports that the server already runs a network
// diagnostic; Agents execute at most one at a time.
var ErrNetworkDiagnosticBusy = errors.New("服务器已有网络诊断在执行，请稍后再试")

// CreateExternalOutboundCheckTask atomically queues an isolated proxy probe and its result slot.
func (s *Store) CreateExternalOutboundCheckTask(ctx context.Context, server model.Server, task *model.AgentTask) error {
	if task.Type != model.AgentTaskTypeProbeExternalEgress || task.ServerID != server.ID {
		return errors.New("invalid external outbound check task")
	}
	var plan model.ExternalEgressProbePlan
	if json.Unmarshal([]byte(task.PayloadJSON), &plan) != nil || plan.Standalone == nil {
		return errors.New("missing standalone probe")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = runtimeSecurityServerTx(ctx, tx, server); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, "select count(*) from agent_tasks where server_id=? and type in ('probe_network_trace','probe_network_ping','probe_network_tcp','probe_network_dns','probe_network_http','probe_google_region','probe_external_egress') and status in ('pending','running')", server.ID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrNetworkDiagnosticBusy
	}
	task.ID, err = operationTaskInsert(ctx, tx, *task, now())
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `insert into external_outbound_probe_results(external_outbound_id,fingerprint,task_id,status,checked_at) values(?,?,?,'pending',?) on conflict(external_outbound_id) do update set fingerprint=excluded.fingerprint,task_id=excluded.task_id,status='pending',exit_ip='',region_code='',error='',checked_at=excluded.checked_at`, plan.Standalone.ExternalOutboundID, plan.Standalone.Fingerprint, task.ID, now())
	if err != nil {
		return err
	}
	return tx.Commit()
}
