package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/OboardProject/oboard/internal/model"
)

func uniqueSortedInt64s(groups ...[]int64) []int64 {
	seen := map[int64]struct{}{}
	for _, group := range groups {
		for _, id := range group {
			if id > 0 {
				seen[id] = struct{}{}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]int64, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// PrunedDisabledProxyPathsResult reports what one automatic sweep of disabled
// proxy paths removed or deferred.
type PrunedDisabledProxyPathsResult struct {
	DeletedPathIDs []int64
	SkippedPathIDs []int64
	TemplateIDs    []int64
}

// PruneDisabledProxyPaths removes abandoned disabled proxy paths (excluding
// template-owned family_branch rows) together with their steps, subscription
// plan membership, access exceptions, node metadata and audit history links.
//
// When updatedBefore is non-zero, only rows whose updated_at is at or before
// updatedBefore are eligible so transient disabled drafts in flight are left
// untouched. Paths whose owning plan is currently applying an access change
// are skipped and reported in SkippedPathIDs so the caller can retry later.
func (s *Store) PruneDisabledProxyPaths(ctx context.Context, updatedBefore time.Time) (PrunedDisabledProxyPathsResult, error) {
	rows, err := s.db.QueryContext(ctx, `select id, updated_at from proxy_paths where enabled=0 and coalesce(kind,'chain')!=? and template_id is null order by id asc`, model.ProxyPathKindFamilyBranch)
	if err != nil {
		return PrunedDisabledProxyPathsResult{}, err
	}
	type candidate struct {
		id        int64
		updatedAt time.Time
	}
	candidates := []candidate{}
	for rows.Next() {
		var item candidate
		var ua string
		if err := rows.Scan(&item.id, &ua); err != nil {
			return PrunedDisabledProxyPathsResult{}, errors.Join(err, rows.Close())
		}
		item.updatedAt = parseTime(ua)
		if !updatedBefore.IsZero() && !item.updatedAt.IsZero() && item.updatedAt.After(updatedBefore) {
			continue
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return PrunedDisabledProxyPathsResult{}, errors.Join(err, rows.Close())
	}
	if err := rows.Close(); err != nil {
		return PrunedDisabledProxyPathsResult{}, err
	}
	if len(candidates) == 0 {
		return PrunedDisabledProxyPathsResult{}, nil
	}

	var result PrunedDisabledProxyPathsResult
	templateSet := map[int64]struct{}{}
	for _, item := range candidates {
		deleted, templates, skipped, err := s.pruneOneDisabledProxyPath(ctx, item.id, updatedBefore)
		if err != nil {
			return result, err
		}
		if skipped {
			result.SkippedPathIDs = append(result.SkippedPathIDs, item.id)
			continue
		}
		result.DeletedPathIDs = append(result.DeletedPathIDs, deleted...)
		for _, templateID := range templates {
			templateSet[templateID] = struct{}{}
		}
	}
	result.DeletedPathIDs = uniqueSortedInt64s(result.DeletedPathIDs)
	result.SkippedPathIDs = uniqueSortedInt64s(result.SkippedPathIDs)
	for templateID := range templateSet {
		result.TemplateIDs = append(result.TemplateIDs, templateID)
	}
	sort.Slice(result.TemplateIDs, func(i, j int) bool { return result.TemplateIDs[i] < result.TemplateIDs[j] })
	if err := s.detachConnectionAuditHistory(ctx, "path_id", "proxy_path", result.DeletedPathIDs); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Store) pruneOneDisabledProxyPath(ctx context.Context, pathID int64, updatedBefore time.Time) ([]int64, []int64, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, false, err
	}
	defer tx.Rollback()

	var enabled int
	var kind string
	var hasTemplate int
	var ua string
	err = tx.QueryRowContext(ctx, `select enabled, coalesce(kind,'chain'), template_id is not null, updated_at from proxy_paths where id=?`, pathID).Scan(&enabled, &kind, &hasTemplate, &ua)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	if enabled != 0 || model.ProxyPathKind(kind) == model.ProxyPathKindFamilyBranch || hasTemplate != 0 {
		return nil, nil, false, nil
	}
	updatedAt := parseTime(ua)
	if !updatedBefore.IsZero() && !updatedAt.IsZero() && updatedAt.After(updatedBefore) {
		return nil, nil, false, nil
	}

	// Detach any surviving branch (including live branches that copied an earlier
	// prefix) so cleaning up one disabled path never takes down an enabled path.
	if _, err := tx.ExecContext(ctx, `update proxy_paths set branch_source_step_id=null,updated_at=? where id!=? and branch_source_step_id in (select id from proxy_path_steps where path_id=?)`, now(), pathID, pathID); err != nil {
		return nil, nil, false, err
	}
	templateIDs, err := queryInt64sTx(ctx, tx, `select distinct family_split_template_id from routing_rules where family_split_template_id is not null and (proxy_path_id=? or target_proxy_path_id=?)`, pathID, pathID)
	if err != nil {
		return nil, nil, false, err
	}
	deletedPathIDs, err := cascadeDeleteProxyPathBranchesCollectTx(ctx, tx, nil, []int64{pathID})
	if err != nil {
		if errors.Is(err, ErrPlanVersionApplying) {
			return nil, nil, true, nil
		}
		return nil, nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, false, err
	}
	return deletedPathIDs, templateIDs, false, nil
}

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
// branches), paths that traverse it as a hop, probe results, node metadata and
// access exceptions. Provider DNS records are released by the caller first,
// because a network failure must not leave a half-deleted inbound.
func (s *Store) DeleteInbound(ctx context.Context, inboundID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := removeAssignableNodeFromPlansTx(ctx, tx, model.AssignableNodeInbound, inboundID); err != nil {
		return err
	}
	deletedPathIDs, err := deleteProxyPathsForInboundTx(ctx, tx, inboundID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from assignable_node_metadata where node_type=? and node_id=?`, string(model.AssignableNodeInbound), inboundID); err != nil {
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
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := s.detachConnectionAuditHistory(ctx, "path_id", "proxy_path", deletedPathIDs); err != nil {
		return err
	}
	return s.detachConnectionAuditHistory(ctx, "inbound_id", "inbound", []int64{inboundID})
}

// DeleteExternalOutbound removes an imported node globally: plan membership,
// every path that uses it, its metadata and access exceptions, in one
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
	deletedPathIDs, err := truncateProxyPathStepsTx(ctx, tx, `select path_id,min(position) from proxy_path_steps where external_outbound_id=? group by path_id`, externalID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from assignable_node_metadata where node_type=? and node_id=?`, string(model.AssignableNodeExternalOutbound), externalID); err != nil {
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
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.detachConnectionAuditHistory(ctx, "path_id", "proxy_path", deletedPathIDs)
}
