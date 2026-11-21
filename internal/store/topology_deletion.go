package store

import (
	"context"
	"database/sql"

	"github.com/OboardProject/oboard/internal/model"
)

// danglingUserNodeExceptionsSQL selects access exceptions whose node no longer
// exists. Inbound, proxy path and external outbound IDs are never reused, so a
// dangling row cannot grant anything today, but it is shown to operators and
// would silently re-attach if an ID were ever reused.
const danglingUserNodeExceptionsSQL = `(node_type='inbound' and not exists(select 1 from inbounds i where i.id=user_node_exceptions.node_id))
	or (node_type='proxy_path' and not exists(select 1 from proxy_paths p where p.id=user_node_exceptions.node_id))
	or (node_type='external_outbound' and not exists(select 1 from external_outbounds e where e.id=user_node_exceptions.node_id))`

func pruneDanglingUserNodeExceptionsTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `delete from user_node_exceptions where `+danglingUserNodeExceptionsSQL)
	return err
}

// PruneDanglingUserNodeExceptions removes access exceptions left behind by
// deletions that predate the in-transaction cleanup. It is idempotent.
func (s *Store) PruneDanglingUserNodeExceptions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `delete from user_node_exceptions where `+danglingUserNodeExceptionsSQL)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// detachConnectionAuditHistory clears history references before the referenced
// rows go away. History lives in a separate database, so it cannot join the
// main transaction; a failure queues the cleanup instead of blocking deletion.
func (s *Store) detachConnectionAuditHistory(ctx context.Context, column, kind string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	history, err := s.historyDB()
	if err == nil {
		_, err = history.ExecContext(ctx, `update connection_audit_reports set `+column+`=null where `+column+` in (`+inClause(len(ids))+`)`, seenAny(ids)...)
	}
	if err != nil {
		for _, id := range ids {
			if queueErr := s.QueueHistoryCleanup(ctx, kind, id); queueErr != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteInbound removes one inbound together with everything that depends on
// it in a single transaction: plan membership, paths rooted at it (and their
// branches), the suffix of paths that traverse it as a hop, probe results and
// access exceptions. Provider DNS records are released by the caller first,
// because a network failure must not leave a half-deleted inbound.
func (s *Store) DeleteInbound(ctx context.Context, inboundID int64) error {
	rootPathIDs, err := queryInt64sDB(ctx, s.db, `select id from proxy_paths where inbound_id=?`, inboundID)
	if err != nil {
		return err
	}
	if err := s.detachConnectionAuditHistory(ctx, "path_id", "proxy_path", rootPathIDs); err != nil {
		return err
	}
	if err := s.detachConnectionAuditHistory(ctx, "inbound_id", "inbound", []int64{inboundID}); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := removeAssignableNodeFromPlansTx(ctx, tx, model.AssignableNodeInbound, inboundID); err != nil {
		return err
	}
	if err := deleteProxyPathsForInboundTx(ctx, tx, inboundID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from inbound_probe_results where inbound_id=?`, inboundID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `delete from inbounds where id=?`, inboundID)
	if err != nil {
		return err
	}
	if count, err := res.RowsAffected(); err != nil {
		return err
	} else if count == 0 {
		return sql.ErrNoRows
	}
	if err := pruneDanglingUserNodeExceptionsTx(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteExternalOutbound removes an imported node globally: plan membership,
// the suffix of every path that uses it, and its access exceptions, in one
// transaction.
func (s *Store) DeleteExternalOutbound(ctx context.Context, externalID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := removeAssignableNodeFromPlansTx(ctx, tx, model.AssignableNodeExternalOutbound, externalID); err != nil {
		return err
	}
	if err := truncateProxyPathStepsTx(ctx, tx, `select path_id,min(position) from proxy_path_steps where external_outbound_id=? group by path_id`, externalID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `delete from external_outbounds where id=?`, externalID)
	if err != nil {
		return err
	}
	if count, err := res.RowsAffected(); err != nil {
		return err
	} else if count == 0 {
		return sql.ErrNoRows
	}
	if err := pruneDanglingUserNodeExceptionsTx(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
