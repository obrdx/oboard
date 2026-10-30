package controller

import (
	"context"
	"errors"

	"github.com/OboardProject/oboard/internal/model"
)

var errLegacyAuditScoringRetired = errors.New("legacy audit scoring retired; use persisted account audit snapshots")

// auditOverviewData is the retired online-scoring overview. Account audit
// snapshots replaced it, so it answers before reading any setting or history.
func (s *Server) auditOverviewData(ctx context.Context, hours int) (model.ConnectionAuditOverview, model.SubscriptionAuditOverview, model.CombinedAuditOverview, error) {
	return model.ConnectionAuditOverview{}, model.SubscriptionAuditOverview{}, model.CombinedAuditOverview{}, errLegacyAuditScoringRetired
}
