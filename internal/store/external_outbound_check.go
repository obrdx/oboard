package store

import (
	"context"
	"errors"

	"github.com/OboardProject/oboard/internal/model"
)

// ErrNetworkDiagnosticBusy reports that the server already runs a network
// diagnostic; Agents execute at most one at a time.
var ErrNetworkDiagnosticBusy = errors.New("服务器已有网络诊断在执行，请稍后再试")

// CreateExternalOutboundCheckTask queues the monitor-origin TCP probe of a
// third-party node availability check, keeping the one-diagnostic-per-server
// rule shared with probe monitors.
func (s *Store) CreateExternalOutboundCheckTask(ctx context.Context, server model.Server, task *model.AgentTask) error {
	if task.Type != model.AgentTaskTypeNetworkTCP || task.ServerID != server.ID {
		return errors.New("invalid external outbound check task")
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
	if err = tx.QueryRowContext(ctx, "select count(*) from agent_tasks where server_id=? and type in ('probe_network_trace','probe_network_ping','probe_network_tcp','probe_network_dns','probe_network_http','probe_google_region') and status in ('pending','running')", server.ID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrNetworkDiagnosticBusy
	}
	task.ID, err = operationTaskInsert(ctx, tx, *task, now())
	if err != nil {
		return err
	}
	return tx.Commit()
}
