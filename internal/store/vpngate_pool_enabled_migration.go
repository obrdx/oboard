package store

import "context"

// migrateVPNGatePoolEnabledColumn removes the former per-pool enable switch
// (controller-db-20261106-vpngate-pool-enabled). A pool that exists is in
// service, so a row that was switched off becomes active again.
func (s *Store) migrateVPNGatePoolEnabledColumn(ctx context.Context) error {
	present, err := s.tableHasColumn(ctx, "vpngate_pools", "enabled")
	if err != nil || !present {
		return err
	}
	_, err = s.db.ExecContext(ctx, `alter table vpngate_pools drop column enabled`)
	return err
}
