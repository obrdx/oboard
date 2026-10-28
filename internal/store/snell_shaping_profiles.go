package store

import "context"

const snellV6ShapingPresetSeed = `insert or ignore into snell_profiles(name,version,psk,obfs_mode,obfs_host,mode,reuse,tcp_fast_open,shaping_json,remark,builtin,enabled,created_at,updated_at) select 'Snell v6 首包密度避让',6,'','none','','default',0,1,'{"first_record_density":{"mode":"outside","min":0.36,"max":0.65}}','v6 默认整形；每个授权身份使用独立 PSK，主控只挑选首包位密度不在 0.36–0.65 的 PSK，避开实测按位密度 DPI 丢弃区 0.425–0.573。修改策略会轮换不满足的身份 PSK，客户端需更新订阅。客户端需 Surge iOS 5.20+/Mac 6.7+',1,1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z' where not exists (select 1 from snell_profiles where builtin=1 and shaping_json<>'')`

// migrateSnellV6ShapingProfiles retires the built-in unsafe-raw parameter set,
// which every shared Snell inbound rejects, and seeds the shaping preset once
// the shaping_json column exists. Custom parameter sets are left untouched.
func (s *Store) migrateSnellV6ShapingProfiles(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const retired = `select id from snell_profiles where builtin=1 and version=6 and mode='unsafe-raw' and name in ('Snell v6 unsafe-raw','Snell v6 unsafe-raw（测试）')`
	// Only the template link is removed; each inbound keeps its own fields.
	if _, err = tx.ExecContext(ctx, `update inbounds set config_json=json_remove(config_json,'$.snell_profile_id') where protocol='snell' and json_extract(config_json,'$.snell_profile_id') in (`+retired+`)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `delete from snell_profiles where id in (`+retired+`)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, snellV6ShapingPresetSeed); err != nil {
		return err
	}
	return tx.Commit()
}
