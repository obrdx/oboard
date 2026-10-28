package store

import "context"

func (s *Store) migrateSnellV6IdentityProfiles(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const retired = `select id from snell_profiles where builtin=1 and version=6 and name in ('Snell v6 加固-中（抗 DPI）','Snell v6 加固-高（抗 DPI）')`
	// Entry parameters have already been materialized; only the template link
	// is retired. Identity credentials and listener configuration stay intact.
	if _, err = tx.ExecContext(ctx, `update inbounds set config_json=json_remove(config_json,'$.snell_profile_id') where protocol='snell' and json_extract(config_json,'$.snell_profile_id') in (`+retired+`)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `delete from snell_profiles where id in (`+retired+`)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `update snell_profiles set remark=? where builtin=1 and version=6 and name in ('Snell v6 标准','Snell v6 标准（测试）') and (remark like 'v6 标准模式（低，未加固）%' or remark='v6 标准模式；测试版协议，客户端需 Surge iOS 5.20+/Mac 6.7+')`, "v6 默认整形模式；每个授权身份使用独立 PSK，整形特征随该凭据变化。预设不保证抗 DPI 效果。"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `update snell_profiles set remark=? where builtin=1 and version=6 and name in ('Snell v6 unshaped','Snell v6 unshaped（测试）') and remark like 'v6 无整形模式；%'`, "v6 无整形模式；每个授权身份使用独立 PSK。"); err != nil {
		return err
	}
	return tx.Commit()
}
