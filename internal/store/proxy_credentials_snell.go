package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
	"github.com/OboardProject/oboard/internal/security"
)

// A credential's material never changes, so its verdict against one policy
// is cached by credential ID until the cache is reset.
type snellShapingVerdictKey struct {
	id     string
	policy model.SnellShapingPolicy
}

var (
	snellShapingVerdicts     sync.Map
	snellShapingVerdictCount atomic.Int64
)

type proxyCredentialRowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func credentialMeetsSnellShaping(ctx context.Context, q proxyCredentialRowQuerier, secret, id string, policy model.SnellShapingPolicy) (bool, error) {
	if policy.IsZero() {
		return true, nil
	}
	key := snellShapingVerdictKey{id, policy}
	if verdict, ok := snellShapingVerdicts.Load(key); ok {
		return verdict.(bool), nil
	}
	var encrypted string
	if err := q.QueryRowContext(ctx, `select material_encrypted from proxy_credentials where id=? and status='active'`, id).Scan(&encrypted); err != nil {
		return false, err
	}
	plain, err := security.DecryptSecret(secret, "proxy-credential:"+id, encrypted)
	if err != nil {
		return false, err
	}
	var material proxyCredentialMaterial
	if err := json.Unmarshal([]byte(plain), &material); err != nil {
		return false, err
	}
	verdict := core.SnellPSKMeetsShaping(material.Password, policy)
	if snellShapingVerdictCount.Add(1) > 65536 {
		snellShapingVerdicts.Clear()
		snellShapingVerdictCount.Store(0)
	}
	snellShapingVerdicts.Store(key, verdict)
	return verdict, nil
}

// prepareShapedSnellPasswords selects PSKs for the shaped Snell scopes that
// will need new material, before the reconcile transaction opens, so the
// selection work never holds the database write lock. The transaction
// re-derives the same decisions and selects inline for anything missed here.
func (s *Store) prepareShapedSnellPasswords(ctx context.Context, secret string, scopes map[proxyCredentialScope]model.ProxyCredential) (map[proxyCredentialScope]string, error) {
	shaped := map[proxyCredentialScope]model.SnellShapingPolicy{}
	for scope, c := range scopes {
		if c.Protocol == model.ProtocolSnell && !c.SnellShaping.IsZero() {
			shaped[scope] = c.SnellShaping
		}
	}
	if len(shaped) == 0 {
		return nil, nil
	}
	existing, err := listProxyCredentials(ctx, s.db)
	if err != nil {
		return nil, err
	}
	covered := map[proxyCredentialScope]bool{}
	for _, c := range existing {
		policy, ok := shaped[credentialScope(c)]
		if c.Status != "active" || !ok {
			continue
		}
		meets, err := credentialMeetsSnellShaping(ctx, s.db, secret, c.ID, policy)
		if err != nil {
			return nil, err
		}
		if meets {
			covered[credentialScope(c)] = true
		}
	}
	type job struct {
		scope  proxyCredentialScope
		policy model.SnellShapingPolicy
	}
	jobs := make(chan job)
	var (
		mu       sync.Mutex
		out      = map[proxyCredentialScope]string{}
		firstErr error
		wg       sync.WaitGroup
	)
	for range max(1, min(runtime.GOMAXPROCS(0), 4)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				psk, err := core.NewShapedSnellPSK(j.policy)
				mu.Lock()
				if err != nil && firstErr == nil {
					firstErr = err
				} else if err == nil {
					out[j.scope] = psk
				}
				mu.Unlock()
			}
		}()
	}
	for scope, policy := range shaped {
		if !covered[scope] {
			jobs <- job{scope, policy}
		}
	}
	close(jobs)
	wg.Wait()
	return out, firstErr
}
