package controller

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OboardProject/oboard/internal/application"
	"github.com/OboardProject/oboard/internal/authorization"
	"github.com/OboardProject/oboard/internal/mcpauth"
	"github.com/OboardProject/oboard/internal/model"
	"github.com/OboardProject/oboard/internal/security"
	"github.com/OboardProject/oboard/internal/store"
)

const (
	oauthAuthorizationCodeTTL = 5 * time.Minute
	oauthAccessTokenTTL       = 15 * time.Minute
	oauthRefreshTokenTTL      = 30 * 24 * time.Hour
	// oauthRefreshReplayGrace is how long a rotated refresh token keeps
	// returning the token pair it produced. Multi-process clients present the
	// same stored refresh token twice within milliseconds; replaying the
	// winning pair keeps that race out of reuse detection while bounding how
	// long a stolen, already-rotated token stays useful.
	oauthRefreshReplayGrace = 10 * time.Second
	// oauthConsumedCodeRetention is how long a redeemed authorization code is
	// remembered so a second redemption revokes the tokens the first produced.
	oauthConsumedCodeRetention = 10 * time.Minute

	oauthAuthorizationMetadataPath = "/.well-known/oauth-authorization-server"
	oauthProtectedResourcePath     = "/.well-known/oauth-protected-resource"
)

func (s *Server) registerOAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc(oauthAuthorizationMetadataPath, s.oauthAuthorizationMetadata)
	mux.HandleFunc(oauthProtectedResourcePath, s.oauthProtectedResourceMetadata)
	mux.HandleFunc("/oauth/authorize", s.oauthAuthorizeEntry)
	mux.HandleFunc("/oauth/token", s.oauthToken)
	mux.HandleFunc("/oauth/revoke", s.oauthRevoke)
	// DCR is retired. The route remains during the migration window to return a
	// structured 410 so old clients fail loudly instead of silently dropping
	// their write scopes; the final version removes the route entirely.
	mux.HandleFunc("/oauth/register", s.oauthDynamicRegisterGone)
}

func (s *Server) registerOAuthManagementRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/oauth-clients", s.auth(s.oauthClients, model.RoleAdmin))
	mux.HandleFunc("/api/v1/oauth-clients/", s.auth(s.oauthClient, model.RoleAdmin))
	mux.HandleFunc("/api/v1/oauth-grants", s.auth(s.oauthGrants, model.RoleAdmin))
	mux.HandleFunc("/api/v1/oauth-grants/", s.auth(s.oauthGrant, model.RoleAdmin))
}

func (s *Server) oauthAuthorizationMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		oauthError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	base, err := s.publicBaseURL(r.Context())
	if err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                         base,
		"authorization_endpoint":                         base + "/oauth/authorize",
		"token_endpoint":                                 base + "/oauth/token",
		"revocation_endpoint":                            base + "/oauth/revoke",
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"revocation_endpoint_auth_methods_supported":     []string{"none"},
		"client_id_metadata_document_supported":          true,
		"authorization_response_iss_parameter_supported": true,
		"scopes_supported":                               []string{mcpauth.ScopeRead, mcpauth.ScopeOperate, mcpauth.ScopeOffline},
		"resource":                                       base + "/api/v1/mcp",
	})
}

func oauthProtectedResourceMetadataURL(base string) string {
	return oauthWellKnownURL(base, oauthProtectedResourcePath, "/api/v1/mcp")
}

func oauthWellKnownURL(base, prefix, suffix string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return ""
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	parsed.Path = prefix + basePath + suffix
	parsed.RawPath = ""
	return parsed.String()
}

func (s *Server) matchOAuthWellKnownPath(requestPath string) (string, string, bool) {
	state := s.basePathState()
	basePaths := []string{state.Current}
	if state.MigrationVersion > 0 && state.Previous != state.Current {
		basePaths = append(basePaths, state.Previous)
	}
	for _, basePath := range basePaths {
		if requestPath == oauthAuthorizationMetadataPath+basePath {
			return basePath, oauthAuthorizationMetadataPath, true
		}
		if requestPath == oauthProtectedResourcePath+basePath+"/api/v1/mcp" {
			return basePath, oauthProtectedResourcePath, true
		}
	}
	return "", "", false
}

func (s *Server) oauthProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		oauthError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	base, err := s.publicBaseURL(r.Context())
	if err != nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 base + "/api/v1/mcp",
		"authorization_servers":    []string{base},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{mcpauth.ScopeRead, mcpauth.ScopeOperate},
		"resource_documentation":   base + "/docs/mcp",
	})
}

// oauthDynamicRegisterGone reports that dynamic client registration is retired
// and instructs the caller to use CIMD (client_id metadata document) or a
// pre-registered client.
func (s *Server) oauthDynamicRegisterGone(w http.ResponseWriter, r *http.Request) {
	oauthError(w, http.StatusGone, "invalid_client_metadata", "dynamic client registration is retired; use a Client ID Metadata Document (CIMD) or a pre-registered client")
}

func (s *Server) oauthClients(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.oauthRegister(w, r)
		return
	}
	if r.Method != http.MethodGet {
		v2Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不受支持")
		return
	}
	items, err := s.store.ListOAuthClients(r.Context())
	if err != nil {
		v2HandleError(w, r, err)
		return
	}
	v2Write(w, r, http.StatusOK, items, map[string]any{"count": len(items)})
}

func (s *Server) oauthRegister(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
		MetadataURI  string   `json:"metadata_uri"`
	}
	if !decodeV2(w, r, &input) {
		return
	}
	item, err := s.newOAuthClient(input.ClientName, input.RedirectURIs)
	if err != nil {
		v2HandleError(w, r, err)
		return
	}
	if metadataURI := strings.TrimSpace(input.MetadataURI); metadataURI != "" {
		metadata, fetchErr := s.fetchClientMetadata(r.Context(), metadataURI)
		if fetchErr != nil {
			v2Error(w, r, http.StatusBadRequest, "invalid_client_metadata", fetchErr.Error())
			return
		}
		item.MetadataURI = metadataURI
		item.MetadataHash = metadata.hash
		item.MetadataETag = metadata.etag
		item.MetadataFetchedAt = metadata.fetchedAt
		item.IdentityType = "cimd"
		item.RedirectURIs = metadata.redirectURIs
	}
	if err := s.store.CreateOAuthClient(r.Context(), item); err != nil {
		v2HandleError(w, r, err)
		return
	}
	s.auditOAuthEvent(r, oauthAuditActor(currentUser(r)), "oauth_client_created", "oauth_client", item.ID, map[string]any{"client_name": boundedOAuthAuditValue(item.Name), "redirect_uris": item.RedirectURIs, "identity_type": item.IdentityType})
	v2Write(w, r, http.StatusCreated, item, nil)
}

func (s *Server) oauthGrants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		v2Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不受支持")
		return
	}
	items, err := s.store.ListOAuthGrants(r.Context())
	if err != nil {
		v2HandleError(w, r, err)
		return
	}
	items = slices.DeleteFunc(items, func(item model.OAuthGrant) bool {
		return item.RevokedAt != nil || item.Status == model.OAuthGrantRevoked
	})
	for index := range items {
		user, userErr := s.store.GetUser(r.Context(), items[index].UserID)
		if userErr != nil {
			continue
		}
		role, roleErr := s.store.EffectiveUserRole(r.Context(), *user)
		if roleErr != nil {
			continue
		}
		items[index].EffectiveRole = role
		items[index].AccessLevel = string(mcpGrantAccessLevel(items[index].AccessLevel, role))
		if boundary, marshalErr := json.Marshal(s.oauthRoleBoundary(role)); marshalErr == nil {
			items[index].ResourceBoundaryJSON = boundary
		}
	}
	_ = s.store.EnrichOAuthGrantStats(r.Context(), items, time.Now().UTC())
	v2Write(w, r, http.StatusOK, items, map[string]any{"count": len(items)})
}

func (s *Server) oauthGrant(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/oauth-grants/"), "/")
	if path == "" || strings.Contains(strings.TrimSuffix(path, "/revoke-offline-access"), "/") {
		v2Error(w, r, http.StatusNotFound, "not_found", "OAuth Grant 不存在")
		return
	}
	if strings.HasSuffix(path, "/revoke-offline-access") {
		id := strings.TrimSuffix(path, "/revoke-offline-access")
		if r.Method != http.MethodPost {
			v2Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不受支持")
			return
		}
		if err := s.store.RevokeOAuthOfflineAccess(r.Context(), id, time.Now().UTC()); err != nil {
			v2HandleError(w, r, err)
			return
		}
		auditReq(s, r, "revoke_offline_access", "oauth_grant", id)
		v2Write(w, r, http.StatusOK, map[string]any{"id": id, "offline_access": false}, nil)
		return
	}
	id := path
	if r.Method != http.MethodDelete {
		v2Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不受支持")
		return
	}
	if err := s.store.RevokeOAuthAuthorization(r.Context(), id, time.Now().UTC()); err != nil {
		v2HandleError(w, r, err)
		return
	}
	s.closeMCPTerminalsForGrant(id)
	auditReq(s, r, "revoke", "oauth_grant", id)
	v2Write(w, r, http.StatusOK, map[string]any{"id": id, "revoked": true}, nil)
}

// newOAuthClient creates a client identity record. It never stores a
// permission ceiling; a client may request whatever the consenting user's role
// allows at grant time.
func (s *Server) newOAuthClient(name string, redirects []string) (*model.OAuthClient, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 || len(redirects) == 0 || len(redirects) > 16 {
		return nil, errors.New("client_name and redirect_uris are required")
	}
	seen := map[string]bool{}
	validated := make([]string, 0, len(redirects))
	for _, raw := range redirects {
		raw = strings.TrimSpace(raw)
		u, err := url.Parse(raw)
		if err != nil || u.Fragment != "" || u.User != nil || u.Host == "" {
			return nil, errors.New("redirect URIs must use HTTPS or an HTTP loopback address")
		}
		if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackRedirectHost(u.Hostname())) {
			return nil, errors.New("redirect URIs must use HTTPS or an HTTP loopback address")
		}
		if u.Scheme == "https" && isLoopbackRedirectHost(u.Hostname()) && u.Port() == "" {
			return nil, errors.New("loopback HTTPS redirect URIs require an explicit port")
		}
		if !seen[raw] {
			seen[raw] = true
			validated = append(validated, raw)
		}
	}
	if len(validated) == 0 {
		return nil, errors.New("at least one redirect URI is required")
	}
	random, err := security.RandomToken(18)
	if err != nil {
		return nil, err
	}
	return &model.OAuthClient{ID: "oc_" + random, Name: name, RedirectURIs: validated, IdentityType: "preregistered", ClientMetadata: json.RawMessage(`{}`), Enabled: true}, nil
}

func isLoopbackRedirectHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) writeMCPAuthenticationRequired(w http.ResponseWriter, r *http.Request, invalidToken bool) {
	base, err := s.publicBaseURL(r.Context())
	if err != nil {
		v2Error(w, r, http.StatusServiceUnavailable, "oauth_metadata_unavailable", err.Error())
		return
	}
	challenge := "Bearer resource_metadata=" + strconv.Quote(oauthProtectedResourceMetadataURL(base)) + `, scope=` + strconv.Quote(mcpChallengeScope)
	code, message := "unauthorized", "需要 OAuth 登录"
	if invalidToken {
		challenge += `, error="invalid_token", error_description="The access token is invalid or expired"`
		code, message = "invalid_token", "访问 Token 无效或已过期"
	}
	w.Header().Set("WWW-Authenticate", challenge)
	v2Error(w, r, http.StatusUnauthorized, code, message)
}

// mcpChallengeScope is the scope set an unauthenticated MCP request is asked to
// obtain. offline_access is deliberately absent: it is an authorization-server
// scope the client requests on its own, not a resource requirement.
const mcpChallengeScope = mcpauth.ScopeRead + " " + mcpauth.ScopeOperate

type oauthAuthorizationRequest struct {
	ClientID      string
	RedirectURI   string
	Scope         []string
	State         string
	Resource      string
	CodeChallenge string
	Issuer        string
}

// oauthAuthorizationError is an authorization endpoint failure. redirect is set
// only once client_id and redirect_uri are verified, which is the point after
// which OAuth 2.1 returns errors to the client's callback instead of the user.
type oauthAuthorizationError struct {
	code        string
	description string
	redirect    bool
}

func (e *oauthAuthorizationError) Error() string { return e.description }

func oauthAuthorizeFailure(code, description string, redirect bool) error {
	return &oauthAuthorizationError{code: code, description: description, redirect: redirect}
}

// oauthLoginRedirectWriter suppresses the session middleware's 401 so a browser
// without a panel session can be sent to the login page instead.
type oauthLoginRedirectWriter struct {
	http.ResponseWriter
	intercept  bool
	suppressed bool
}

func (w *oauthLoginRedirectWriter) WriteHeader(status int) {
	if w.intercept && status == http.StatusUnauthorized {
		w.suppressed = true
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *oauthLoginRedirectWriter) Write(body []byte) (int, error) {
	if w.suppressed {
		return len(body), nil
	}
	return w.ResponseWriter.Write(body)
}

// oauthAuthorizeEntry requires a panel session for the authorization endpoint.
// A browser GET without one is redirected to the panel login page, which
// returns to this exact same-origin authorization request after sign-in.
func (s *Server) oauthAuthorizeEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		s.auth(s.oauthAuthorize, model.RoleViewer)(w, r)
		return
	}
	writer := &oauthLoginRedirectWriter{ResponseWriter: w, intercept: true}
	s.auth(func(inner http.ResponseWriter, request *http.Request) {
		writer.intercept = false
		s.oauthAuthorize(inner, request)
	}, model.RoleViewer)(writer, r)
	if writer.suppressed {
		s.redirectOAuthLogin(w, r)
	}
}

func (s *Server) redirectOAuthLogin(w http.ResponseWriter, r *http.Request) {
	basePath := requestBasePath(r, s.currentBasePath())
	target := basePath + "/oauth/authorize"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	w.Header().Del("Content-Type")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", basePath+"/?oauth_return="+url.QueryEscape(target))
	w.WriteHeader(http.StatusFound)
}

func (s *Server) oauthAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		oauthError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "invalid authorization request")
		return
	}
	user := currentUser(r)
	request, client, err := s.validateOAuthAuthorizationRequest(r)
	if err != nil {
		failure := &oauthAuthorizationError{code: "invalid_request", description: err.Error()}
		_ = errors.As(err, &failure)
		s.auditOAuthEvent(r, oauthAuditActor(user), "oauth_authorization_denied", "oauth_client", boundedOAuthAuditValue(r.Form.Get("client_id")), map[string]any{"reason": failure.code})
		if failure.redirect {
			oauthRedirectError(w, r, request, failure.code, failure.description)
			return
		}
		oauthError(w, http.StatusBadRequest, failure.code, failure.description)
		return
	}
	if r.Method == http.MethodGet {
		if user == nil {
			s.auditOAuthEvent(r, nil, "oauth_authorization_denied", "oauth_client", client.ID, map[string]any{"reason": "login_required"})
			oauthError(w, http.StatusUnauthorized, "access_denied", "login required")
			return
		}
		if err := s.validateOAuthUserGrant(r.Context(), user, request.Scope); err != nil {
			s.auditOAuthEvent(r, oauthAuditActor(user), "oauth_authorization_denied", "oauth_client", client.ID, map[string]any{"reason": "scope_denied", "scopes": request.Scope})
			oauthRedirectError(w, r, request, "access_denied", err.Error())
			return
		}
		s.renderOAuthConsent(w, r, request, client)
		return
	}
	if r.Form.Get("decision") != "approve" {
		s.auditOAuthEvent(r, oauthAuditActor(user), "oauth_authorization_denied", "oauth_client", client.ID, map[string]any{"reason": "user_denied", "scopes": request.Scope})
		oauthRedirectError(w, r, request, "access_denied", "")
		return
	}
	if user == nil {
		s.auditOAuthEvent(r, nil, "oauth_authorization_denied", "oauth_client", client.ID, map[string]any{"reason": "login_required"})
		oauthError(w, http.StatusUnauthorized, "access_denied", "login required")
		return
	}
	if err := s.validateOAuthUserGrant(r.Context(), user, request.Scope); err != nil {
		s.auditOAuthEvent(r, &user.ID, "oauth_authorization_denied", "oauth_client", client.ID, map[string]any{"reason": "scope_denied", "scopes": request.Scope})
		oauthRedirectError(w, r, request, "access_denied", err.Error())
		return
	}
	grant, principal, profile, policies, err := s.buildOAuthGrantMaterial(r, *user, *client, request.Scope)
	if err != nil {
		s.auditOAuthEvent(r, &user.ID, "oauth_authorization_denied", "oauth_client", client.ID, map[string]any{"reason": "invalid_consent", "scopes": request.Scope})
		oauthRedirectError(w, r, request, "invalid_request", err.Error())
		return
	}
	rawCode, err := security.RandomToken(32)
	if err != nil {
		oauthRedirectError(w, r, request, "server_error", "")
		return
	}
	code := &model.OAuthAuthorizationCode{
		CodeHash: security.HashAPISecret(s.sessionSecret, rawCode), ClientID: client.ID, UserID: user.ID,
		RedirectURI: request.RedirectURI, Resource: request.Resource, CodeChallenge: request.CodeChallenge,
		Scopes: request.Scope, ExpiresAt: time.Now().UTC().Add(oauthAuthorizationCodeTTL),
	}
	_, offline, scopeErr := normalizeRequestedScopes(request.Scope)
	if scopeErr != nil {
		s.auditOAuthEvent(r, &user.ID, "oauth_authorization_denied", "oauth_client", client.ID, map[string]any{"reason": "invalid_consent", "scopes": request.Scope})
		oauthRedirectError(w, r, request, "invalid_scope", scopeErr.Error())
		return
	}
	boundary := s.oauthRoleBoundary(mustEffectiveRole(r.Context(), s.store, *user))
	grant, principal, err = s.store.AuthorizeOAuthClient(r.Context(), store.OAuthAuthorizeRequest{
		ClientID: client.ID, UserID: user.ID, ResourceKey: model.OAuthResourceKeyMCP, ResourceURL: request.Resource,
		RequestedScopes: request.Scope, RequestOffline: offline, AccessLevel: grant.AccessLevel,
		BoundaryJSON: grant.ResourceBoundaryJSON, PrincipalResourceFilter: application.ResourceFilterFromBoundary(boundary),
		NewGrant: grant, NewPrincipal: principal, NewProfile: profile, NewPolicies: policies, Code: code,
	})
	if err != nil {
		target := ""
		if grant != nil {
			target = grant.ID
		}
		s.auditOAuthEvent(r, &user.ID, "oauth_authorization_denied", "oauth_grant", target, map[string]any{"client_id": client.ID, "reason": "authorize_failed"})
		oauthRedirectError(w, r, request, "server_error", "")
		return
	}
	s.auditOAuthEvent(r, &user.ID, "oauth_authorization_granted", "oauth_grant", grant.ID, map[string]any{"client_id": client.ID, "scopes": mcpauth.ParseAccessLevel(grant.AccessLevel).NormalizedScopes(grant.OfflineAccess), "resource": request.Resource, "access_level": grant.AccessLevel})
	s.renderOAuthSuccess(w, r, request, rawCode, client.Name)
}

func (s *Server) validateOAuthAuthorizationRequest(r *http.Request) (oauthAuthorizationRequest, *model.OAuthClient, error) {
	request := oauthAuthorizationRequest{ClientID: strings.TrimSpace(r.Form.Get("client_id")), RedirectURI: r.Form.Get("redirect_uri"), Scope: strings.Fields(r.Form.Get("scope")), State: r.Form.Get("state"), Resource: r.Form.Get("resource"), CodeChallenge: r.Form.Get("code_challenge")}
	base, baseErr := s.publicBaseURL(r.Context())
	if baseErr == nil {
		request.Issuer = base
	}
	client, err := s.resolveOAuthClient(r, request.ClientID, true)
	if err != nil {
		return request, nil, oauthAuthorizeFailure("invalid_request", err.Error(), false)
	}
	if !oauthRedirectURIMatches(client.RedirectURIs, request.RedirectURI) {
		return request, nil, oauthAuthorizeFailure("invalid_request", "redirect_uri does not match the registered client", false)
	}
	if baseErr != nil {
		return request, nil, oauthAuthorizeFailure("temporarily_unavailable", baseErr.Error(), true)
	}
	if r.Form.Get("response_type") != "code" {
		return request, nil, oauthAuthorizeFailure("unsupported_response_type", "only the authorization code flow is supported", true)
	}
	if r.Form.Get("code_challenge_method") != "S256" || len(request.CodeChallenge) < 43 || len(request.CodeChallenge) > 128 {
		return request, nil, oauthAuthorizeFailure("invalid_request", "authorization code with PKCE S256 is required", true)
	}
	resource := canonicalOAuthResource(base + "/api/v1/mcp")
	if strings.TrimSpace(request.Resource) == "" {
		request.Resource = resource
	} else if canonicalOAuthResource(request.Resource) != resource {
		return request, nil, oauthAuthorizeFailure("invalid_target", "resource is not this MCP server", true)
	}
	request.Resource = resource
	if len(request.Scope) == 0 {
		request.Scope = strings.Fields(mcpChallengeScope)
	}
	for _, scope := range request.Scope {
		if !mcpauth.KnownScope(scope) {
			return request, nil, oauthAuthorizeFailure("invalid_scope", "unsupported OAuth scope: "+scope, true)
		}
	}
	return request, client, nil
}

// resolveOAuthClient finds the client a request names. A pre-registered client
// is addressed by its oc_ identifier. An HTTPS client_id is a Client ID
// Metadata Document: a known document is revalidated when stale, and, when
// discover is set, an unknown one is fetched with the SSRF-safe fetcher and
// recorded under an oc_ identifier with the document URL as metadata_uri, so
// administrators can see and disable it like any other client.
func (s *Server) resolveOAuthClient(r *http.Request, raw string, discover bool) (*model.OAuthClient, error) {
	raw = strings.TrimSpace(raw)
	unknown := errors.New("unknown or disabled client")
	if raw == "" || len(raw) > 2048 {
		return nil, unknown
	}
	client, err := s.store.GetOAuthClientByIdentifier(r.Context(), raw)
	if err == nil {
		if !client.Enabled {
			return nil, unknown
		}
		if discover && client.IdentityType == "cimd" {
			if refreshErr := s.refreshClientMetadataIfStale(r.Context(), client); refreshErr != nil {
				return nil, errors.New("client metadata could not be revalidated: " + refreshErr.Error())
			}
		}
		return client, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if !discover || !strings.HasPrefix(raw, "https://") {
		return nil, unknown
	}
	if !s.allowRateRaw("oauth-cimd:"+clientIP(r), 10, time.Minute) {
		return nil, errors.New("too many client metadata lookups; retry later")
	}
	metadata, err := s.fetchClientMetadata(r.Context(), raw)
	if err != nil {
		return nil, err
	}
	random, err := security.RandomToken(18)
	if err != nil {
		return nil, err
	}
	item := &model.OAuthClient{
		ID: "oc_" + random, Name: truncateOAuthClientName(metadata.clientName), RedirectURIs: metadata.redirectURIs,
		IdentityType: "cimd", MetadataURI: raw, MetadataHash: metadata.hash, MetadataETag: metadata.etag,
		MetadataFetchedAt: metadata.fetchedAt, ClientMetadata: json.RawMessage(`{}`), Enabled: true,
	}
	if err := s.store.CreateOAuthClient(r.Context(), item); err != nil {
		if existing, getErr := s.store.GetOAuthClientByIdentifier(r.Context(), raw); getErr == nil {
			if !existing.Enabled {
				return nil, unknown
			}
			return existing, nil
		}
		return nil, err
	}
	s.auditOAuthEvent(r, oauthAuditActor(currentUser(r)), "oauth_client_discovered", "oauth_client", item.ID, map[string]any{"client_name": boundedOAuthAuditValue(item.Name), "redirect_uris": item.RedirectURIs, "identity_type": item.IdentityType})
	return item, nil
}

// oauthClientIdentity maps the client_id a token request presents to the stored
// client identifier without fetching anything.
func (s *Server) oauthClientIdentity(ctx context.Context, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if client, err := s.store.GetOAuthClientByIdentifier(ctx, raw); err == nil {
		return client.ID
	}
	return raw
}

func truncateOAuthClientName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Remote MCP Client"
	}
	runes := []rune(name)
	if len(runes) > 80 {
		return string(runes[:80])
	}
	return name
}

// oauthRedirectURIMatches compares a requested callback with the registered
// ones exactly, except that an HTTP loopback callback may use any port
// (OAuth 2.1 section 8.4.2): native clients bind an ephemeral port per run.
func oauthRedirectURIMatches(registered []string, requested string) bool {
	if requested == "" {
		return false
	}
	if slices.Contains(registered, requested) {
		return true
	}
	candidate, err := url.Parse(requested)
	if err != nil || candidate.Scheme != "http" || candidate.User != nil || candidate.Fragment != "" || !isLoopbackRedirectHost(candidate.Hostname()) {
		return false
	}
	for _, raw := range registered {
		allowed, parseErr := url.Parse(raw)
		if parseErr != nil || allowed.Scheme != "http" || !isLoopbackRedirectHost(allowed.Hostname()) {
			continue
		}
		if strings.EqualFold(allowed.Hostname(), candidate.Hostname()) && allowed.EscapedPath() == candidate.EscapedPath() && allowed.RawQuery == candidate.RawQuery {
			return true
		}
	}
	return false
}

// canonicalOAuthResource normalizes a resource indicator for comparison: scheme
// and host are case-insensitive, a default port is implied, and a trailing
// slash carries no meaning. The path otherwise stays case-sensitive.
func canonicalOAuthResource(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return strings.TrimSpace(raw)
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if scheme == "https" && port == "443" || scheme == "http" && port == "80" {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	out := url.URL{Scheme: scheme, Host: host, Path: strings.TrimRight(parsed.Path, "/"), RawQuery: parsed.RawQuery}
	return out.String()
}

// normalizeRequestedScopes canonicalizes the client's coarse scope request.
// operate implies read. A request above the user's role is narrowed to the
// role, and the token response's scope reports what was actually granted.
func normalizeRequestedScopes(requested []string) (mcpauth.AccessLevel, bool, error) {
	operate := mcpauth.RequestsOperate(requested)
	offline := mcpauth.RequestsOffline(requested)
	for _, scope := range requested {
		if !mcpauth.KnownScope(scope) {
			return "", false, errors.New("unsupported OAuth scope: " + scope)
		}
	}
	level := mcpauth.AccessRead
	if operate {
		level = mcpauth.AccessOperate
	}
	return level, offline, nil
}

// buildOAuthGrantMaterial prepares a new grant bundle for first-time authorization.
// AuthorizeOAuthClient reuses an existing live grant instead of creating duplicates.
func (s *Server) buildOAuthGrantMaterial(r *http.Request, user model.User, client model.OAuthClient, scopes []string) (*model.OAuthGrant, *model.APIPrincipal, *model.OAuthApprovalProfile, []model.ApprovalPolicy, error) {
	_, offline, err := normalizeRequestedScopes(scopes)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	role, err := s.store.EffectiveUserRole(r.Context(), user)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	accessLevel := mcpRequestedAccessLevel(scopes, role)
	if accessLevel == "" {
		return nil, nil, nil, nil, errors.New("the current role does not permit MCP access")
	}
	boundary := s.oauthRoleBoundary(role)
	const autoRisk = 3
	grantToken, err := security.RandomToken(18)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	principalToken, err := security.RandomToken(18)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	profileToken, err := security.RandomToken(18)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	boundaryJSON, err := json.Marshal(boundary.Normalized())
	if err != nil {
		return nil, nil, nil, nil, err
	}
	appPrincipal := &model.APIPrincipal{ID: "oauth_" + principalToken, OwnerUserID: &user.ID, Name: client.Name + " / " + user.Username, Type: model.APIPrincipalOAuth, Enabled: true, Scopes: accessLevel.NormalizedScopes(offline), ResourceFilter: application.ResourceFilterFromBoundary(boundary), RateLimitPerMinute: 120, MaxConcurrency: 4}
	profile := &model.OAuthApprovalProfile{ID: "oap_" + profileToken, Name: "OAuth grant approval", AutoApproveRisk: autoRisk}
	grant := &model.OAuthGrant{
		ID: "grt_" + grantToken, ClientID: client.ID, ClientName: client.Name, UserID: user.ID, Username: user.Username,
		PrincipalID: appPrincipal.ID, AccessLevel: string(accessLevel), ResourceBoundaryJSON: boundaryJSON,
		ApprovalProfileID: profile.ID, OfflineAccess: offline, PolicyVersion: 1, RoleVersion: 1,
		ConsentVersion: 2, Status: model.OAuthGrantActive, ResourceKey: model.OAuthResourceKeyMCP,
	}
	principalForEval := application.Principal{ID: appPrincipal.ID, UserID: &user.ID, Name: user.Username, Type: model.APIPrincipalOAuth, Role: role, Scopes: accessLevel.NormalizedScopes(offline), ResourceFilter: appPrincipal.ResourceFilter, AccessLevel: accessLevel}
	policies := []model.ApprovalPolicy{}
	for _, descriptor := range s.capabilities.ListMCP(principalForEval) {
		if !descriptor.Executable {
			continue
		}
		policyToken, randomErr := security.RandomToken(18)
		if randomErr != nil {
			return nil, nil, nil, nil, randomErr
		}
		mode := model.ApprovalAutomatic
		policies = append(policies, model.ApprovalPolicy{ID: "pol_" + policyToken, PrincipalID: appPrincipal.ID, Capability: descriptor.Name, ResourceFilter: json.RawMessage(`{}`), Mode: mode})
	}
	return grant, appPrincipal, profile, policies, nil
}

// createOAuthGrantV2 creates an OAuth identity whose access is the requested
// scope capped by the consenting user's role; RBAC still applies per call.
func (s *Server) createOAuthGrantV2(r *http.Request, user model.User, client model.OAuthClient, scopes []string) (*model.OAuthGrant, *model.APIPrincipal, error) {
	grant, principal, profile, policies, err := s.buildOAuthGrantMaterial(r, user, client, scopes)
	if err != nil {
		return nil, nil, err
	}
	if err := s.store.CreateOAuthGrantV2(r.Context(), grant, principal, profile, policies); err != nil {
		return nil, nil, err
	}
	return grant, principal, nil
}

func mustEffectiveRole(ctx context.Context, db *store.Store, user model.User) model.Role {
	role, err := db.EffectiveUserRole(ctx, user)
	if err != nil {
		return user.Role
	}
	return role
}

// mcpGrantAccessLevel is the access a grant actually has: the level its OAuth
// scope requested, capped by what the owning user's current role allows. A role
// downgrade narrows it at once; a role upgrade never widens a read grant.
func mcpGrantAccessLevel(grantLevel string, role model.Role) mcpauth.AccessLevel {
	roleLevel := mcpAccessLevelForRole(role)
	if roleLevel == "" {
		return ""
	}
	if mcpauth.ParseAccessLevel(grantLevel) == mcpauth.AccessOperate && roleLevel == mcpauth.AccessOperate {
		return mcpauth.AccessOperate
	}
	return mcpauth.AccessRead
}

// mcpRequestedAccessLevel caps the requested scope by the consenting role.
func mcpRequestedAccessLevel(scopes []string, role model.Role) mcpauth.AccessLevel {
	requested := mcpauth.AccessRead
	if mcpauth.RequestsOperate(scopes) {
		requested = mcpauth.AccessOperate
	}
	return mcpGrantAccessLevel(string(requested), role)
}

func mcpAccessLevelForRole(role model.Role) mcpauth.AccessLevel {
	if authorization.RoleRank(role) >= 2 {
		return mcpauth.AccessOperate
	}
	if authorization.RoleRank(role) >= 1 {
		return mcpauth.AccessRead
	}
	return ""
}

func (s *Server) oauthRoleBoundary(role model.Role) mcpauth.ResourceBoundary {
	accessLevel := mcpAccessLevelForRole(role)
	resources := map[string]mcpauth.ResourceSelection{}
	for _, resourceType := range []string{"audit_incident", "certificate", "deployment", "external_outbound", "inbound", "proxy_path", "server", "subscription", "user", "workflow", "changeset"} {
		resources[resourceType] = mcpauth.ResourceSelection{Selection: mcpauth.SelectionAll, IncludeFuture: true}
	}
	globalCapabilities := []string{}
	principal := application.Principal{AccessLevel: accessLevel, Role: role, Scopes: []string{"*"}}
	for _, descriptor := range s.capabilities.ListMCP(principal) {
		if !s.capabilities.RBAC().Allows(role, descriptor.RBACPermission) {
			continue
		}
		if len(descriptor.ResourceTypes) == 0 {
			globalCapabilities = append(globalCapabilities, descriptor.Name)
			continue
		}
		for _, resourceType := range descriptor.ResourceTypes {
			resources[resourceType] = mcpauth.ResourceSelection{Selection: mcpauth.SelectionAll, IncludeFuture: true}
		}
	}
	serverSelection := resources["server"]
	serverSelection.AllowCreate = s.capabilities.RBAC().Allows(role, "servers.onboard")
	resources["server"] = serverSelection
	return mcpauth.ResourceBoundary{Version: mcpauth.ResourceBoundaryVersion, Resources: resources, GlobalCapabilities: globalCapabilities, DestructiveOperations: false}.Normalized()
}

func (s *Server) oauthToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		oauthError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	if !s.allowRate(w, r, "oauth-token:"+clientIP(r), 30, time.Minute) {
		return
	}
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "invalid token request")
		return
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		s.oauthExchangeCode(w, r)
	case "refresh_token":
		s.oauthExchangeRefresh(w, r)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant_type")
	}
}

func (s *Server) oauthExchangeCode(w http.ResponseWriter, r *http.Request) {
	clientID := s.oauthClientIdentity(r.Context(), r.Form.Get("client_id"))
	codeHash := security.HashAPISecret(s.sessionSecret, r.Form.Get("code"))
	now := time.Now().UTC()
	code, err := s.store.ConsumeOAuthAuthorizationCode(r.Context(), codeHash, now, oauthConsumedCodeRetention)
	if errors.Is(err, store.ErrOAuthCodeReplay) {
		s.auditOAuthEvent(r, nil, "oauth_code_reuse", "oauth_client", boundedOAuthAuditValue(clientID), map[string]any{"flow": "authorization_code", "reason": "tokens_revoked"})
		oauthError(w, http.StatusBadRequest, "invalid_grant", "authorization code was already used; tokens issued from it are revoked")
		return
	}
	if err != nil || !code.ExpiresAt.After(now) || code.ClientID != clientID || code.RedirectURI != r.Form.Get("redirect_uri") || !pkceMatches(code.CodeChallenge, r.Form.Get("code_verifier")) || !oauthResourceMatches(r.Form.Get("resource"), code.Resource) {
		s.auditOAuthEvent(r, nil, "oauth_token_denied", "oauth_client", boundedOAuthAuditValue(clientID), map[string]any{"flow": "authorization_code", "reason": "invalid_grant"})
		oauthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid")
		return
	}
	s.issueOAuthTokens(w, r, oauthTokenIssue{flow: "authorization_code", grantID: code.GrantID, principalID: code.PrincipalID, clientID: code.ClientID, userID: code.UserID, resource: code.Resource, requestOffline: mcpauth.RequestsOffline(code.Scopes), codeHash: codeHash})
}

// oauthResourceMatches accepts an omitted resource parameter on the token
// endpoint (the grant already carries its audience) and otherwise compares the
// normalized indicator with the stored one.
func oauthResourceMatches(requested, stored string) bool {
	if strings.TrimSpace(requested) == "" {
		return true
	}
	return canonicalOAuthResource(requested) == canonicalOAuthResource(stored)
}

// oauthRefreshGate serializes rotations that present the same refresh token.
// waiters keeps the gate alive while requests are queued behind it.
type oauthRefreshGate struct {
	mu      sync.Mutex
	waiters int
}

// oauthRefreshReplay is the token pair one refresh token produced, kept for the
// replay grace window so a client that lost the rotation race can still be
// answered with the credentials that actually won.
type oauthRefreshReplay struct {
	response  map[string]any
	clientID  string
	resource  string
	expiresAt time.Time
}

func (s *Server) lockOAuthRefresh(tokenHash string) func() {
	s.oauthRefreshMu.Lock()
	if s.oauthRefreshGates == nil {
		s.oauthRefreshGates = map[string]*oauthRefreshGate{}
	}
	gate := s.oauthRefreshGates[tokenHash]
	if gate == nil {
		gate = &oauthRefreshGate{}
		s.oauthRefreshGates[tokenHash] = gate
	}
	gate.waiters++
	s.oauthRefreshMu.Unlock()
	gate.mu.Lock()
	return func() {
		gate.mu.Unlock()
		s.oauthRefreshMu.Lock()
		gate.waiters--
		if gate.waiters == 0 {
			delete(s.oauthRefreshGates, tokenHash)
		}
		s.oauthRefreshMu.Unlock()
	}
}

func (s *Server) rememberOAuthRefreshReplay(tokenHash, clientID, resource string, response map[string]any, at time.Time) {
	if tokenHash == "" || s.oauthRefreshGrace <= 0 {
		return
	}
	s.oauthRefreshMu.Lock()
	defer s.oauthRefreshMu.Unlock()
	if s.oauthRefreshReplays == nil {
		s.oauthRefreshReplays = map[string]oauthRefreshReplay{}
	}
	for hash, entry := range s.oauthRefreshReplays {
		if !entry.expiresAt.After(at) {
			delete(s.oauthRefreshReplays, hash)
		}
	}
	s.oauthRefreshReplays[tokenHash] = oauthRefreshReplay{response: maps.Clone(response), clientID: clientID, resource: resource, expiresAt: at.Add(s.oauthRefreshGrace)}
}

func (s *Server) oauthRefreshReplayResponse(tokenHash, clientID, resource string, at time.Time) (map[string]any, bool) {
	s.oauthRefreshMu.Lock()
	defer s.oauthRefreshMu.Unlock()
	entry, ok := s.oauthRefreshReplays[tokenHash]
	if !ok {
		return nil, false
	}
	if !entry.expiresAt.After(at) {
		delete(s.oauthRefreshReplays, tokenHash)
		return nil, false
	}
	if entry.clientID != clientID || entry.resource != resource {
		return nil, false
	}
	return maps.Clone(entry.response), true
}

func (s *Server) oauthExchangeRefresh(w http.ResponseWriter, r *http.Request) {
	tokenHash := security.HashAPISecret(s.sessionSecret, r.Form.Get("refresh_token"))
	clientID, resource := s.oauthClientIdentity(r.Context(), r.Form.Get("client_id")), r.Form.Get("resource")
	// Serialize rotations of one refresh token so a second caller observes a
	// finished rotation and its cached result instead of a half-written one.
	defer s.lockOAuthRefresh(tokenHash)()
	at := time.Now().UTC()
	refresh, err := s.store.ConsumeOAuthRefreshToken(r.Context(), tokenHash, at, s.oauthRefreshGrace)
	if refresh != nil && oauthResourceMatches(resource, refresh.Resource) {
		resource = refresh.Resource
	}
	if errors.Is(err, store.ErrOAuthRefreshReplay) {
		s.oauthReplayRefresh(w, r, refresh, tokenHash, clientID, resource, at)
		return
	}
	if errors.Is(err, store.ErrOAuthRefreshReuse) {
		var actor *int64
		target := ""
		if refresh != nil {
			actor, target = &refresh.UserID, refresh.GrantID
		}
		s.auditOAuthEvent(r, actor, "oauth_refresh_reuse", "oauth_grant", target, map[string]any{"client_id": boundedOAuthAuditValue(clientID), "reason": "token_family_revoked"})
		oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token reuse detected; token family revoked")
		return
	}
	if err != nil || refresh.ClientID != clientID || refresh.Resource != resource {
		var actor *int64
		target := boundedOAuthAuditValue(clientID)
		if refresh != nil {
			actor, target = &refresh.UserID, refresh.GrantID
		}
		s.auditOAuthEvent(r, actor, "oauth_token_denied", "oauth_grant", target, map[string]any{"flow": "refresh_token", "reason": "invalid_grant"})
		oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid")
		return
	}
	s.issueOAuthTokens(w, r, oauthTokenIssue{flow: "refresh_token", grantID: refresh.GrantID, principalID: refresh.PrincipalID, clientID: refresh.ClientID, userID: refresh.UserID, resource: refresh.Resource, familyID: refresh.FamilyID, parentTokenHash: refresh.TokenHash, requestOffline: true})
}

// oauthReplayRefresh answers a refresh token that was already rotated inside the
// grace window. A cached pair is returned verbatim; without one the single
// request fails while the family stays intact, so the process that won the
// rotation keeps its session and the loser can retry with the newest token.
func (s *Server) oauthReplayRefresh(w http.ResponseWriter, r *http.Request, refresh *model.OAuthToken, tokenHash, clientID, resource string, at time.Time) {
	if refresh == nil || refresh.ClientID != clientID || refresh.Resource != resource {
		var actor *int64
		target := boundedOAuthAuditValue(clientID)
		if refresh != nil {
			actor, target = &refresh.UserID, refresh.GrantID
		}
		s.auditOAuthEvent(r, actor, "oauth_token_denied", "oauth_grant", target, map[string]any{"flow": "refresh_token", "reason": "invalid_grant"})
		oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid")
		return
	}
	response, ok := s.oauthRefreshReplayResponse(tokenHash, clientID, resource, at)
	if !ok {
		s.auditOAuthEvent(r, &refresh.UserID, "oauth_token_denied", "oauth_grant", refresh.GrantID, map[string]any{"client_id": boundedOAuthAuditValue(clientID), "flow": "refresh_token", "reason": "refresh_replay_unavailable"})
		oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token was already rotated; retry with the newest refresh token")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	s.auditOAuthEvent(r, &refresh.UserID, "oauth_refresh_replayed", "oauth_grant", refresh.GrantID, map[string]any{"client_id": boundedOAuthAuditValue(clientID), "flow": "refresh_token", "resource": resource})
	writeJSON(w, http.StatusOK, response)
}

// oauthTokenIssue describes one token endpoint answer. codeHash is set for an
// authorization code exchange so a concurrent replay of the same code can
// revoke what this exchange issues.
type oauthTokenIssue struct {
	flow, grantID, principalID, clientID string
	userID                               int64
	resource, familyID, parentTokenHash  string
	requestOffline                       bool
	codeHash                             string
}

func (s *Server) issueOAuthTokens(w http.ResponseWriter, r *http.Request, issue oauthTokenIssue) {
	flow, grantID, principalID, clientID, userID := issue.flow, issue.grantID, issue.principalID, issue.clientID, issue.userID
	resource, familyID, parentTokenHash, requestOffline := issue.resource, issue.familyID, issue.parentTokenHash, issue.requestOffline
	grant, err := s.store.GetOAuthGrant(r.Context(), grantID)
	if err != nil || grant.RevokedAt != nil || grant.Status != model.OAuthGrantActive || grant.ExpiresAt != nil && !grant.ExpiresAt.After(time.Now().UTC()) || grant.PrincipalID != principalID || grant.ClientID != clientID || grant.UserID != userID {
		s.auditOAuthEvent(r, &userID, "oauth_token_denied", "oauth_grant", grantID, map[string]any{"client_id": clientID, "flow": flow, "reason": "inactive_grant"})
		oauthError(w, http.StatusBadRequest, "invalid_grant", "OAuth grant is no longer active")
		return
	}
	client, err := s.store.GetOAuthClient(r.Context(), clientID)
	if err != nil || !client.Enabled {
		s.auditOAuthEvent(r, &userID, "oauth_token_denied", "oauth_grant", grantID, map[string]any{"client_id": clientID, "flow": flow, "reason": "disabled_client"})
		oauthError(w, http.StatusBadRequest, "invalid_grant", "OAuth client is disabled")
		return
	}
	user, err := s.store.GetUser(r.Context(), userID)
	if err != nil {
		s.auditOAuthEvent(r, &userID, "oauth_token_denied", "oauth_grant", grantID, map[string]any{"client_id": clientID, "flow": flow, "reason": "user_not_authorized"})
		oauthError(w, http.StatusBadRequest, "invalid_grant", "user is no longer authorized")
		return
	}
	effectiveRole, err := s.store.EffectiveUserRole(r.Context(), *user)
	if err != nil || mcpAccessLevelForRole(effectiveRole) == "" {
		s.auditOAuthEvent(r, &userID, "oauth_token_denied", "oauth_grant", grantID, map[string]any{"client_id": clientID, "flow": flow, "reason": "user_not_authorized"})
		oauthError(w, http.StatusBadRequest, "invalid_grant", "user is no longer authorized for the requested scopes")
		return
	}
	accessRaw, err := security.RandomToken(32)
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	now := time.Now().UTC()
	accessPlain := "oba_" + accessRaw
	access := &model.OAuthToken{TokenHash: security.HashAPISecret(s.sessionSecret, accessPlain), GrantID: grantID, PrincipalID: principalID, ClientID: clientID, UserID: userID, Resource: resource, ExpiresAt: now.Add(oauthAccessTokenTTL)}
	var refresh *model.OAuthToken
	refreshPlain := ""
	issueRefresh := grant.OfflineAccess && (flow == "refresh_token" || requestOffline)
	if issueRefresh {
		refreshRaw, randomErr := security.RandomToken(32)
		if randomErr != nil {
			oauthError(w, http.StatusInternalServerError, "server_error", randomErr.Error())
			return
		}
		if familyID == "" {
			familyID, _ = security.RandomToken(18)
		}
		refreshPlain = "obr_" + refreshRaw
		refresh = &model.OAuthToken{TokenHash: security.HashAPISecret(s.sessionSecret, refreshPlain), FamilyID: familyID, GrantID: grantID, ParentTokenHash: parentTokenHash, PrincipalID: principalID, ClientID: clientID, UserID: userID, Resource: resource, ExpiresAt: now.Add(oauthRefreshTokenTTL)}
	}
	if err := s.store.CreateOAuthTokens(r.Context(), access, refresh); err != nil {
		s.auditOAuthEvent(r, &userID, "oauth_token_denied", "oauth_grant", grantID, map[string]any{"client_id": clientID, "flow": flow, "reason": "token_issue_failed"})
		oauthError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	if issue.codeHash != "" {
		refreshFamily := ""
		if refresh != nil {
			refreshFamily = refresh.FamilyID
		}
		replayed, err := s.store.RecordOAuthCodeTokens(r.Context(), issue.codeHash, access.TokenHash, refreshFamily, now)
		if err != nil || replayed {
			s.auditOAuthEvent(r, &userID, "oauth_code_reuse", "oauth_grant", grantID, map[string]any{"client_id": clientID, "flow": flow, "reason": "tokens_revoked"})
			oauthError(w, http.StatusBadRequest, "invalid_grant", "authorization code was already used; tokens issued from it are revoked")
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	scopes := mcpGrantAccessLevel(grant.AccessLevel, effectiveRole).NormalizedScopes(grant.OfflineAccess)
	response := map[string]any{"access_token": accessPlain, "token_type": "Bearer", "expires_in": int(oauthAccessTokenTTL.Seconds()), "scope": strings.Join(scopes, " "), "resource": resource}
	if refreshPlain != "" {
		response["refresh_token"] = refreshPlain
	}
	action := "oauth_token_issued"
	if flow == "refresh_token" {
		action = "oauth_token_refreshed"
		s.rememberOAuthRefreshReplay(parentTokenHash, clientID, resource, response, now)
	}
	s.auditOAuthEvent(r, &userID, action, "oauth_grant", grantID, map[string]any{"client_id": clientID, "flow": flow, "offline_access": refreshPlain != "", "resource": resource, "scope": strings.Join(scopes, " ")})
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) oauthRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		oauthError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "invalid revocation request")
		return
	}
	// Public clients authenticate a revocation only by client_id (RFC 7009
	// section 2.1): a token is revoked only for the client it was issued to,
	// and a mismatch is answered like an unknown token.
	clientID := s.oauthClientIdentity(r.Context(), r.Form.Get("client_id"))
	if clientID == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "client_id is required")
		return
	}
	outcome := "ignored"
	if err := s.store.RevokeOAuthToken(r.Context(), security.HashAPISecret(s.sessionSecret, r.Form.Get("token")), clientID); err == nil {
		outcome = "revoked"
	}
	s.auditOAuthEvent(r, nil, "oauth_token_revoked", "oauth_client", clientID, map[string]any{"outcome": outcome})
	w.WriteHeader(http.StatusOK)
}

func (s *Server) auditOAuthEvent(r *http.Request, actorID *int64, action, target, targetID string, detail map[string]any) {
	detail["target_id"] = boundedOAuthAuditValue(targetID)
	encoded, err := json.Marshal(detail)
	if err != nil {
		return
	}
	_ = s.store.AddAudit(r.Context(), model.AuditLog{ActorID: actorID, Action: action, Target: target, Detail: string(encoded), IP: clientIP(r)})
}

func oauthAuditActor(user *model.User) *int64 {
	if user == nil {
		return nil
	}
	return &user.ID
}

func boundedOAuthAuditValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 128 {
		return value[:128]
	}
	return value
}

func (s *Server) validateOAuthUserGrant(ctx context.Context, user *model.User, scopes []string) error {
	if user == nil || user.Status != "active" {
		return errors.New("active user is required")
	}
	role, err := s.store.EffectiveUserRole(ctx, *user)
	if err != nil {
		return err
	}
	if _, _, err := normalizeRequestedScopes(scopes); err != nil {
		return err
	}
	if mcpAccessLevelForRole(role) == "" {
		return errors.New("the current role does not permit MCP access")
	}
	return nil
}

// allCapabilityScopes lists the legacy fine-grained scopes for Service Account
// scope validation. MCP never uses these scopes.
func (s *Server) allCapabilityScopes() []string {
	seen := map[string]bool{"offline_access": true}
	for _, descriptor := range s.capabilities.List(application.Principal{Scopes: []string{"*"}}) {
		for _, scope := range descriptor.RequiredScopes {
			seen[scope] = true
		}
	}
	out := make([]string, 0, len(seen))
	for scope := range seen {
		out = append(out, scope)
	}
	slices.Sort(out)
	return out
}

func pkceMatches(challenge, verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return challenge == base64.RawURLEncoding.EncodeToString(sum[:])
}

// oauthRedirectError returns an authorization error to the verified client
// callback (OAuth 2.1 section 4.1.2.1), carrying iss for mix-up defence.
func oauthRedirectError(w http.ResponseWriter, r *http.Request, request oauthAuthorizationRequest, oauthErr, description string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, oauthRedirectLocation(request, "", oauthErr, description), http.StatusFound)
}

func oauthRedirectLocation(request oauthAuthorizationRequest, code, oauthErr, description string) string {
	u, _ := url.Parse(request.RedirectURI)
	query := u.Query()
	if request.State != "" {
		query.Set("state", request.State)
	}
	if request.Issuer != "" {
		query.Set("iss", request.Issuer)
	}
	if oauthErr != "" {
		query.Set("error", oauthErr)
		if description != "" {
			query.Set("error_description", description)
		}
	} else {
		query.Set("code", code)
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func oauthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
