package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/OboardProject/oboard/internal/model"
)

var (
	ErrOAuthRefreshReuse = errors.New("oauth refresh token reuse detected")
	// ErrOAuthRefreshReplay reports a refresh token that was already rotated
	// inside the replay grace window. The token family stays intact so a client
	// that lost a concurrent rotation keeps a working session.
	ErrOAuthRefreshReplay = errors.New("oauth refresh token replayed inside grace window")
	// ErrOAuthCodeReplay reports a second redemption of an authorization code.
	// The tokens the first redemption issued have already been revoked.
	ErrOAuthCodeReplay = errors.New("oauth authorization code replayed")
)

func (s *Store) CreateOAuthClient(ctx context.Context, item *model.OAuthClient) error {
	redirects, err := json.Marshal(item.RedirectURIs)
	if err != nil {
		return err
	}
	ts := now()
	if item.IdentityType == "" {
		item.IdentityType = "preregistered"
	}
	_, err = s.db.ExecContext(ctx, `insert into oauth_clients(id,name,redirect_uris_json,identity_type,metadata_uri,metadata_hash,metadata_etag,metadata_fetched_at,client_metadata_json,enabled,created_at,updated_at) values(?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.Name, string(redirects), item.IdentityType, item.MetadataURI, item.MetadataHash, item.MetadataETag, timePtrString(item.MetadataFetchedAt), normalizedJSONObject(item.ClientMetadata), boolInt(item.Enabled), ts, ts)
	if err == nil {
		item.CreatedAt, item.UpdatedAt = parseTime(ts), parseTime(ts)
	}
	return err
}

func (s *Store) GetOAuthClient(ctx context.Context, id string) (*model.OAuthClient, error) {
	return scanOAuthClient(s.db.QueryRowContext(ctx, `select id,name,redirect_uris_json,identity_type,coalesce(metadata_uri,''),coalesce(metadata_hash,''),coalesce(metadata_etag,''),metadata_fetched_at,client_metadata_json,enabled,created_at,updated_at from oauth_clients where id=?`, id))
}

// GetOAuthClientByIdentifier resolves the client_id a request presents: a
// stored client ID, or the Client ID Metadata Document URL of a CIMD client.
func (s *Store) GetOAuthClientByIdentifier(ctx context.Context, identifier string) (*model.OAuthClient, error) {
	return scanOAuthClient(s.db.QueryRowContext(ctx, `select id,name,redirect_uris_json,identity_type,coalesce(metadata_uri,''),coalesce(metadata_hash,''),coalesce(metadata_etag,''),metadata_fetched_at,client_metadata_json,enabled,created_at,updated_at from oauth_clients where id=? or (identity_type='cimd' and metadata_uri=?) order by (id=?) desc, created_at limit 1`, identifier, identifier, identifier))
}

func (s *Store) ListOAuthClients(ctx context.Context) ([]model.OAuthClient, error) {
	rows, err := s.db.QueryContext(ctx, `select id,name,redirect_uris_json,identity_type,coalesce(metadata_uri,''),coalesce(metadata_hash,''),coalesce(metadata_etag,''),metadata_fetched_at,client_metadata_json,enabled,created_at,updated_at from oauth_clients order by created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.OAuthClient{}
	for rows.Next() {
		item, err := scanOAuthClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (s *Store) UpdateOAuthClient(ctx context.Context, item *model.OAuthClient) error {
	redirects, err := json.Marshal(item.RedirectURIs)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `update oauth_clients set name=?,redirect_uris_json=?,identity_type=?,metadata_uri=?,metadata_hash=?,metadata_etag=?,metadata_fetched_at=?,enabled=?,updated_at=? where id=?`, item.Name, string(redirects), item.IdentityType, item.MetadataURI, item.MetadataHash, item.MetadataETag, timePtrString(item.MetadataFetchedAt), boolInt(item.Enabled), now(), item.ID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteOAuthClient(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `select principal_id,approval_profile_id from oauth_grants where client_id=?`, id)
	if err != nil {
		return err
	}
	var principalIDs, profileIDs []string
	for rows.Next() {
		var principalID, profileID string
		if err := rows.Scan(&principalID, &profileID); err != nil {
			return errors.Join(err, rows.Close())
		}
		principalIDs = append(principalIDs, principalID)
		profileIDs = append(profileIDs, profileID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `delete from oauth_clients where id=?`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return sql.ErrNoRows
	}
	for _, principalID := range principalIDs {
		if _, err := tx.ExecContext(ctx, `delete from api_principals where id=? and type=?`, principalID, model.APIPrincipalOAuth); err != nil {
			return err
		}
	}
	for _, profileID := range profileIDs {
		if _, err := tx.ExecContext(ctx, `delete from oauth_approval_profiles where id=?`, profileID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanOAuthClient(scanner interface{ Scan(...any) error }) (*model.OAuthClient, error) {
	var item model.OAuthClient
	var redirects, metadata, created, updated string
	var identityType, metadataURI, metadataHash, metadataETag string
	var metadataFetched sql.NullString
	var enabled int
	if err := scanner.Scan(&item.ID, &item.Name, &redirects, &identityType, &metadataURI, &metadataHash, &metadataETag, &metadataFetched, &metadata, &enabled, &created, &updated); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(redirects), &item.RedirectURIs)
	item.IdentityType, item.MetadataURI, item.MetadataHash, item.MetadataETag = identityType, metadataURI, metadataHash, metadataETag
	item.MetadataFetchedAt = nullableTime(metadataFetched)
	item.ClientMetadata = json.RawMessage(metadata)
	item.Enabled = enabled != 0
	item.CreatedAt, item.UpdatedAt = parseTime(created), parseTime(updated)
	return &item, nil
}

// CreateOAuthGrantV2 creates the v2 grant, its audit principal, approval
// profile, and per-capability approval policies in one transaction. Stored
// access and boundary fields are compatibility projections; MCP authorization
// derives from the owning user's current effective role.
func (s *Store) CreateOAuthGrantV2(ctx context.Context, grant *model.OAuthGrant, principal *model.APIPrincipal, profile *model.OAuthApprovalProfile, policies []model.ApprovalPolicy) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := now()
	if err := s.insertOAuthGrantV2Tx(ctx, tx, grant, principal, profile, policies, ts); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateOAuthGrant keeps the legacy signature for tests and migration shims;
// it delegates to the v2 writer after normalizing a legacy scope grant.
func (s *Store) CreateOAuthGrant(ctx context.Context, grant *model.OAuthGrant, principal *model.APIPrincipal, profile *model.OAuthApprovalProfile, policies []model.ApprovalPolicy) error {
	if grant.AccessLevel == "" {
		grant.AccessLevel = "read"
	}
	return s.CreateOAuthGrantV2(ctx, grant, principal, profile, policies)
}

const oauthGrantSelectColumns = `g.id,g.client_id,c.name,g.user_id,u.username,g.principal_id,g.access_level,g.resource_boundary_v2_json,g.approval_profile_id,g.offline_access,g.policy_version,g.role_version,g.consent_version,g.status,g.resource_key,g.expires_at,g.last_used_at,g.last_authorized_at,g.revoked_at,g.revoke_reason,g.created_at,p.id,p.name,p.auto_approve_risk,p.created_at,p.updated_at`

func (s *Store) GetOAuthGrant(ctx context.Context, id string) (*model.OAuthGrant, error) {
	return scanOAuthGrant(s.db.QueryRowContext(ctx, `select `+oauthGrantSelectColumns+` from oauth_grants g join oauth_clients c on c.id=g.client_id join users u on u.id=g.user_id join oauth_approval_profiles p on p.id=g.approval_profile_id where g.id=?`, id))
}

func (s *Store) ListOAuthGrants(ctx context.Context) ([]model.OAuthGrant, error) {
	rows, err := s.db.QueryContext(ctx, `select `+oauthGrantSelectColumns+` from oauth_grants g join oauth_clients c on c.id=g.client_id join users u on u.id=g.user_id join oauth_approval_profiles p on p.id=g.approval_profile_id order by g.created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.OAuthGrant{}
	for rows.Next() {
		item, err := scanOAuthGrant(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanOAuthGrant(scanner interface{ Scan(...any) error }) (*model.OAuthGrant, error) {
	var item model.OAuthGrant
	var boundary, created, profileCreated, profileUpdated string
	var expires, lastUsed, lastAuthorized, revoked sql.NullString
	var offline int
	profile := &model.OAuthApprovalProfile{}
	if err := scanner.Scan(&item.ID, &item.ClientID, &item.ClientName, &item.UserID, &item.Username, &item.PrincipalID, &item.AccessLevel, &boundary, &item.ApprovalProfileID, &offline, &item.PolicyVersion, &item.RoleVersion, &item.ConsentVersion, &item.Status, &item.ResourceKey, &expires, &lastUsed, &lastAuthorized, &revoked, &item.RevokeReason, &created, &profile.ID, &profile.Name, &profile.AutoApproveRisk, &profileCreated, &profileUpdated); err != nil {
		return nil, err
	}
	item.ResourceBoundaryJSON = []byte(boundary)
	item.OfflineAccess = offline != 0
	item.ExpiresAt, item.LastUsedAt, item.LastAuthorizedAt, item.RevokedAt = nullableTime(expires), nullableTime(lastUsed), nullableTime(lastAuthorized), nullableTime(revoked)
	item.CreatedAt = parseTime(created)
	if item.ResourceKey == "" {
		item.ResourceKey = model.OAuthResourceKeyMCP
	}
	profile.CreatedAt, profile.UpdatedAt = parseTime(profileCreated), parseTime(profileUpdated)
	item.ApprovalProfile = profile
	return &item, nil
}

func (s *Store) RevokeOAuthGrant(ctx context.Context, id string, at time.Time) error {
	return s.RevokeOAuthGrantReason(ctx, id, at, "")
}

func (s *Store) RevokeOAuthGrantReason(ctx context.Context, id string, at time.Time, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := at.UTC().Format(time.RFC3339Nano)
	var principalID string
	if err := tx.QueryRowContext(ctx, `select principal_id from oauth_grants where id=?`, id).Scan(&principalID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `update oauth_grants set status=?,revoked_at=coalesce(revoked_at,?),revoke_reason=? where id=? and revoked_at is null`, string(model.OAuthGrantRevoked), ts, reason, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `update api_principals set enabled=0,updated_at=? where id=?`, ts, principalID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `update oauth_access_tokens set revoked_at=? where grant_id=? and revoked_at is null`, ts, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `update oauth_refresh_tokens set revoked_at=? where grant_id=? and revoked_at is null`, ts, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `update mcp_privileged_grants set revoked_at=coalesce(revoked_at,?),updated_at=? where oauth_grant_id=? and revoked_at is null`, ts, ts, id); err != nil {
		return err
	}
	if err := s.invalidateOAuthGrantConsumablesTx(ctx, tx, ts, id); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkOAuthGrantNeedsReconsent marks a grant for mandatory re-consent and
// revokes every token and authorization code for the grant.
func (s *Store) MarkOAuthGrantNeedsReconsent(ctx context.Context, id string, at time.Time, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := at.UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `update oauth_grants set status=?,revoke_reason=? where id=? and status='active'`, string(model.OAuthGrantNeedsReconsent), reason, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `update oauth_access_tokens set revoked_at=? where grant_id=? and revoked_at is null`, ts, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `update oauth_refresh_tokens set revoked_at=? where grant_id=? and revoked_at is null`, ts, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from oauth_authorization_codes where grant_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateOAuthAuthorizationCode(ctx context.Context, item *model.OAuthAuthorizationCode) error {
	ts := now()
	scopesJSON, err := json.Marshal(item.Scopes)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `insert into oauth_authorization_codes(code_hash,grant_id,client_id,user_id,principal_id,redirect_uri,resource,code_challenge,requested_scopes_json,expires_at,created_at) values(?,?,?,?,?,?,?,?,?,?,?)`, item.CodeHash, item.GrantID, item.ClientID, item.UserID, item.PrincipalID, item.RedirectURI, item.Resource, item.CodeChallenge, string(scopesJSON), item.ExpiresAt.UTC().Format(time.RFC3339Nano), ts)
	if err == nil {
		item.CreatedAt = parseTime(ts)
	}
	return err
}

// ConsumeOAuthAuthorizationCode redeems a code once. The redemption is
// remembered for retention so a second presentation of the same code is
// reported as ErrOAuthCodeReplay after revoking what the first one issued
// (OAuth 2.1 section 4.1.3).
func (s *Store) ConsumeOAuthAuthorizationCode(ctx context.Context, codeHash string, at time.Time, retention time.Duration) (*model.OAuthAuthorizationCode, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ts := at.UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `delete from oauth_consumed_codes where expires_at<?`, ts); err != nil {
		return nil, err
	}
	var item model.OAuthAuthorizationCode
	var expires, created, scopesJSON string
	err = tx.QueryRowContext(ctx, `delete from oauth_authorization_codes where code_hash=? returning code_hash,grant_id,client_id,user_id,principal_id,redirect_uri,resource,code_challenge,requested_scopes_json,expires_at,created_at`, codeHash).Scan(&item.CodeHash, &item.GrantID, &item.ClientID, &item.UserID, &item.PrincipalID, &item.RedirectURI, &item.Resource, &item.CodeChallenge, &scopesJSON, &expires, &created)
	if errors.Is(err, sql.ErrNoRows) {
		var grantID, accessHash, family string
		if lookupErr := tx.QueryRowContext(ctx, `update oauth_consumed_codes set replayed_at=coalesce(replayed_at,?) where code_hash=? returning grant_id,access_token_hash,refresh_family_id`, ts, codeHash).Scan(&grantID, &accessHash, &family); lookupErr != nil {
			return nil, err
		}
		if err := revokeOAuthCodeTokensTx(ctx, tx, ts, grantID, accessHash, family); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, ErrOAuthCodeReplay
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `insert into oauth_consumed_codes(code_hash,grant_id,expires_at) values(?,?,?)`, codeHash, item.GrantID, at.Add(retention).UTC().Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(scopesJSON), &item.Scopes)
	item.ExpiresAt, item.CreatedAt = parseTime(expires), parseTime(created)
	return &item, nil
}

// RecordOAuthCodeTokens links the tokens a code redemption issued to that
// code. When the code was replayed while they were being issued, the tokens
// are revoked at once and replayed is true.
func (s *Store) RecordOAuthCodeTokens(ctx context.Context, codeHash, accessTokenHash, refreshFamilyID string, at time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var replayed sql.NullString
	var grantID string
	if err := tx.QueryRowContext(ctx, `update oauth_consumed_codes set access_token_hash=?,refresh_family_id=? where code_hash=? returning grant_id,replayed_at`, accessTokenHash, refreshFamilyID, codeHash).Scan(&grantID, &replayed); err != nil {
		return false, err
	}
	if replayed.Valid {
		if err := revokeOAuthCodeTokensTx(ctx, tx, at.UTC().Format(time.RFC3339Nano), grantID, accessTokenHash, refreshFamilyID); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return replayed.Valid, nil
}

// revokeOAuthCodeTokensTx revokes what a replayed code produced: the refresh
// family it started and every access token of its grant, matching how refresh
// token reuse is handled.
func revokeOAuthCodeTokensTx(ctx context.Context, tx *sql.Tx, ts, grantID, accessTokenHash, refreshFamilyID string) error {
	if _, err := tx.ExecContext(ctx, `update oauth_access_tokens set revoked_at=coalesce(revoked_at,?) where token_hash=? or grant_id=?`, ts, accessTokenHash, grantID); err != nil {
		return err
	}
	if refreshFamilyID != "" {
		if _, err := tx.ExecContext(ctx, `update oauth_refresh_tokens set revoked_at=coalesce(revoked_at,?) where family_id=?`, ts, refreshFamilyID); err != nil {
			return err
		}
	}
	return nil
}

// MigrateOAuthResource rewrites the exact MCP resource audience on every OAuth
// record after a Controller base path migration. Tokens remain usable on the
// new path without reauthorizing, while the audience stays exact per path.
func (s *Store) MigrateOAuthResource(ctx context.Context, from, to string) error {
	if from == "" || to == "" || from == to {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"oauth_access_tokens", "oauth_refresh_tokens", "oauth_authorization_codes"} {
		if _, err := tx.ExecContext(ctx, `update `+table+` set resource=? where resource=?`, to, from); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CreateOAuthTokens(ctx context.Context, access, refresh *model.OAuthToken) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := now()
	if _, err := tx.ExecContext(ctx, `insert into oauth_access_tokens(token_hash,grant_id,principal_id,client_id,user_id,resource,expires_at,created_at) values(?,?,?,?,?,?,?,?)`, access.TokenHash, access.GrantID, access.PrincipalID, access.ClientID, access.UserID, access.Resource, access.ExpiresAt.UTC().Format(time.RFC3339Nano), ts); err != nil {
		return err
	}
	if refresh != nil {
		if _, err := tx.ExecContext(ctx, `insert into oauth_refresh_tokens(token_hash,family_id,grant_id,parent_token_hash,principal_id,client_id,user_id,resource,expires_at,created_at) values(?,?,?,?,?,?,?,?,?,?)`, refresh.TokenHash, refresh.FamilyID, refresh.GrantID, refresh.ParentTokenHash, refresh.PrincipalID, refresh.ClientID, refresh.UserID, refresh.Resource, refresh.ExpiresAt.UTC().Format(time.RFC3339Nano), ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AuthenticateMCPAccessToken resolves an access token for the /api/v1/mcp audience,
// returning the live grant policy, the token, the effective user role, and the
// user status. MCP authorization derives everything from the grant policy; the
// API principal row is kept only for audit identity and rate limiting.
func (s *Store) AuthenticateMCPAccessToken(ctx context.Context, tokenHash, resource string, at time.Time) (token *model.OAuthToken, role, userStatus string, userID int64, err error) {
	row := s.db.QueryRowContext(ctx, `select t.token_hash,t.grant_id,t.principal_id,t.client_id,t.user_id,t.resource,t.expires_at,t.revoked_at,t.created_at,u.role,u.status from oauth_access_tokens t join users u on u.id=t.user_id where t.token_hash=?`, tokenHash)
	var item model.OAuthToken
	var expires, created string
	var revoked sql.NullString
	if err = row.Scan(&item.TokenHash, &item.GrantID, &item.PrincipalID, &item.ClientID, &item.UserID, &item.Resource, &expires, &revoked, &created, &role, &userStatus); err != nil {
		return nil, "", "", 0, err
	}
	item.ExpiresAt, item.RevokedAt, item.CreatedAt = parseTime(expires), nullableTime(revoked), parseTime(created)
	if item.RevokedAt != nil || !item.ExpiresAt.After(at) || item.Resource != resource {
		return nil, "", "", 0, sql.ErrNoRows
	}
	return &item, role, userStatus, item.UserID, nil
}

// ResolveActiveGrant returns the grant row for an active grant along with the
// effective role of the owning user. It verifies client and user state so a
// revoked client or disabled user immediately invalidates every token.
func (s *Store) ResolveActiveGrant(ctx context.Context, grantID string, at time.Time) (*model.OAuthGrant, model.Role, bool, error) {
	row := s.db.QueryRowContext(ctx, `select g.id,g.client_id,g.user_id,g.principal_id,g.access_level,g.resource_boundary_v2_json,g.approval_profile_id,p.auto_approve_risk,g.offline_access,g.policy_version,g.role_version,g.consent_version,g.status,g.expires_at,g.revoked_at,g.created_at,c.enabled,u.status,u.role from oauth_grants g join oauth_clients c on c.id=g.client_id join users u on u.id=g.user_id join oauth_approval_profiles p on p.id=g.approval_profile_id where g.id=?`, grantID)
	var grant model.OAuthGrant
	var boundary, status, created string
	var expires, revoked sql.NullString
	var autoRisk, offline, clientEnabled int
	var userStatus string
	var userRole model.Role
	if err := row.Scan(&grant.ID, &grant.ClientID, &grant.UserID, &grant.PrincipalID, &grant.AccessLevel, &boundary, &grant.ApprovalProfileID, &autoRisk, &offline, &grant.PolicyVersion, &grant.RoleVersion, &grant.ConsentVersion, &status, &expires, &revoked, &created, &clientEnabled, &userStatus, &userRole); err != nil {
		return nil, "", false, err
	}
	grant.OfflineAccess = offline != 0
	grant.Status = model.OAuthGrantStatus(status)
	grant.ResourceBoundaryJSON = []byte(boundary)
	grant.ApprovalProfile = &model.OAuthApprovalProfile{ID: grant.ApprovalProfileID, AutoApproveRisk: autoRisk}
	grant.ExpiresAt = parseNullTime(expires)
	grant.RevokedAt = parseNullTime(revoked)
	grant.CreatedAt = parseTime(created)
	if grant.RevokedAt != nil || grant.Status != model.OAuthGrantActive || grant.ExpiresAt != nil && !grant.ExpiresAt.After(at) || clientEnabled == 0 || userStatus != "active" {
		return nil, "", false, sql.ErrNoRows
	}
	user := model.User{ID: grant.UserID, Status: userStatus, Role: userRole}
	effectiveRole, err := s.EffectiveUserRole(ctx, user)
	if err != nil {
		return nil, "", false, err
	}
	return &grant, effectiveRole, true, nil
}

// ConsumeOAuthRefreshToken rotates a refresh token. A token that was already
// consumed within grace, and whose family is still intact, is reported as
// ErrOAuthRefreshReplay without any state change so a racing client can be
// served the winning rotation; anything else is genuine reuse and revokes the
// family plus every access token of the grant.
func (s *Store) ConsumeOAuthRefreshToken(ctx context.Context, tokenHash string, at time.Time, grace time.Duration) (*model.OAuthToken, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var item model.OAuthToken
	var expires, created string
	var consumed, revoked, reuseDetected sql.NullString
	if err := tx.QueryRowContext(ctx, `select token_hash,family_id,grant_id,parent_token_hash,principal_id,client_id,user_id,resource,expires_at,consumed_at,revoked_at,reuse_detected_at,created_at from oauth_refresh_tokens where token_hash=?`, tokenHash).Scan(&item.TokenHash, &item.FamilyID, &item.GrantID, &item.ParentTokenHash, &item.PrincipalID, &item.ClientID, &item.UserID, &item.Resource, &expires, &consumed, &revoked, &reuseDetected, &created); err != nil {
		return nil, err
	}
	item.ConsumedAt, item.RevokedAt, item.ReuseDetectedAt = nullableTime(consumed), nullableTime(revoked), nullableTime(reuseDetected)
	if item.ConsumedAt != nil {
		if grace > 0 && item.RevokedAt == nil && item.ReuseDetectedAt == nil && !item.ConsumedAt.Add(grace).Before(at) {
			item.ExpiresAt, item.CreatedAt = parseTime(expires), parseTime(created)
			if err := tx.Commit(); err != nil {
				return nil, err
			}
			return &item, ErrOAuthRefreshReplay
		}
		ts := at.UTC().Format(time.RFC3339Nano)
		_, _ = tx.ExecContext(ctx, `update oauth_refresh_tokens set revoked_at=coalesce(revoked_at,?),reuse_detected_at=case when token_hash=? then ? else reuse_detected_at end where family_id=?`, ts, tokenHash, ts, item.FamilyID)
		_, _ = tx.ExecContext(ctx, `update oauth_access_tokens set revoked_at=? where grant_id=? and revoked_at is null`, ts, item.GrantID)
		_ = tx.Commit()
		return &item, ErrOAuthRefreshReuse
	}
	item.ExpiresAt, item.CreatedAt = parseTime(expires), parseTime(created)
	if item.RevokedAt != nil || !item.ExpiresAt.After(at) {
		return nil, sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `update oauth_refresh_tokens set consumed_at=? where token_hash=? and consumed_at is null`, at.UTC().Format(time.RFC3339Nano), tokenHash); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &item, nil
}

// RevokeOAuthToken revokes an access token, or a refresh token's whole family,
// only when it was issued to clientID.
func (s *Store) RevokeOAuthToken(ctx context.Context, tokenHash, clientID string) error {
	ts := now()
	if result, err := s.db.ExecContext(ctx, `update oauth_access_tokens set revoked_at=? where token_hash=? and client_id=? and revoked_at is null`, ts, tokenHash, clientID); err == nil {
		if count, _ := result.RowsAffected(); count > 0 {
			return nil
		}
	} else {
		return err
	}
	var family string
	if err := s.db.QueryRowContext(ctx, `select family_id from oauth_refresh_tokens where token_hash=? and client_id=?`, tokenHash, clientID).Scan(&family); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `update oauth_refresh_tokens set revoked_at=? where family_id=? and revoked_at is null`, ts, family)
	return err
}
