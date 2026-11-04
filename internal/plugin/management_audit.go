package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"

	"github.com/OboardProject/oboard/internal/application"
	"github.com/OboardProject/oboard/internal/model"
)

// Management audit actions. They share plugin_audit_events with SDK calls
// but carry no run UUID, so the instance audit shows who changed what next
// to what the plugin did with it.
const (
	AuditManagePublish  = "manage.publish"
	AuditManageGrant    = "manage.grant"
	AuditManageRevoke   = "manage.grant.revoke"
	AuditManageEnable   = "manage.instance.enable"
	AuditManageDisable  = "manage.instance.disable"
	AuditManageDraftRun = "manage.draft.test_run"
)

// DraftVersion identifies the exact editor draft content. publish binds to
// it so a draft changed after review is never published by accident.
func DraftVersion(manifest []byte, source string) string {
	if len(manifest) == 0 {
		return ""
	}
	sum := sha256.New()
	sum.Write([]byte(strconv.Itoa(len(manifest))))
	sum.Write([]byte{0})
	sum.Write(manifest)
	sum.Write([]byte{0})
	sum.Write([]byte(source))
	return hex.EncodeToString(sum.Sum(nil))
}

// GrantDiff lists capabilities and resource entries added or removed between
// two grants, as stable "capability" or "capability:kind:value" strings.
func GrantDiff(before, after Grant) (added, removed []string) {
	b, a := grantEntries(before), grantEntries(after)
	for entry := range a {
		if !b[entry] {
			added = append(added, entry)
		}
	}
	for entry := range b {
		if !a[entry] {
			removed = append(removed, entry)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	if added == nil {
		added = []string{}
	}
	if removed == nil {
		removed = []string{}
	}
	return added, removed
}

func grantEntries(g Grant) map[string]bool {
	out := map[string]bool{}
	for name, scope := range g.Capabilities {
		out[name] = true
		for _, id := range scope.Servers {
			out[name+":server:"+strconv.FormatInt(id, 10)] = true
		}
		for _, host := range scope.Hosts {
			out[name+":host:"+host] = true
		}
		for _, id := range scope.Channels {
			out[name+":channel:"+strconv.FormatInt(id, 10)] = true
		}
		for _, id := range scope.Users {
			out[name+":user:"+strconv.FormatInt(id, 10)] = true
		}
		for _, id := range scope.Plans {
			out[name+":plan:"+strconv.FormatInt(id, 10)] = true
		}
		for _, id := range scope.Targets {
			out[name+":lan_target:"+id] = true
		}
	}
	return out
}

func actorAuditDetail(actor application.Principal) map[string]any {
	detail := map[string]any{
		"principal_id":   actor.ID,
		"principal_type": string(actor.Type),
		"role":           string(actor.Role),
		"interactive":    actor.Interactive,
	}
	if actor.UserID != nil {
		detail["user_id"] = *actor.UserID
	}
	if actor.GrantID != "" {
		detail["oauth_grant_id"] = actor.GrantID
	}
	if actor.ClientName != "" {
		detail["client_name"] = actor.ClientName
	}
	if actor.SourceIP.IsValid() {
		detail["source_ip"] = actor.SourceIP.String()
	}
	return detail
}

// auditManagement records one management change. A failed audit write is
// returned so a change is never silently unaudited; the caller has already
// committed the change, so it reports the error alongside the result.
func (s *Service) auditManagement(ctx context.Context, actor application.Principal, loaded loadedInstallation, instanceID int64, action string, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	detail["actor"] = actorAuditDetail(actor)
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	version := ""
	if loaded.manifest != nil {
		version = loaded.manifest.Version
	}
	resource := "plugin:" + strconv.FormatInt(loaded.installation.ID, 10)
	if instanceID > 0 {
		resource = "plugin_instance:" + strconv.FormatInt(instanceID, 10)
	}
	return s.store.InsertPluginAuditEvent(ctx, model.PluginAuditEvent{
		InstallationID: loaded.installation.ID, InstanceID: instanceID, PluginKey: loaded.installation.PluginKey,
		PluginVersion: version, Capability: action, Resource: resource, Result: "ok", DetailJSON: raw,
	})
}

// AnyGrantRevision lets a caller replace the grant without naming the
// revision it read; the audit still records the revision it replaced.
const AnyGrantRevision int64 = -1

// GrantInput is the flat MCP grant shape. Capabilities is either a list of
// capability names or a map of capability to scope; Servers fills every
// server-scoped capability that names no servers of its own and LANTargets
// fills lan.request. The result is validated against the manifest by
// SetGrant like any other grant.
type GrantInput struct {
	Capabilities json.RawMessage `json:"capabilities"`
	Servers      []int64         `json:"servers,omitempty"`
	LANTargets   []string        `json:"lan_targets,omitempty"`
}

func (in GrantInput) Grant() (Grant, error) {
	grant := Grant{Capabilities: map[string]CapabilityGrant{}}
	var names []string
	if err := json.Unmarshal(in.Capabilities, &names); err == nil {
		for _, name := range names {
			grant.Capabilities[name] = CapabilityGrant{}
		}
	} else {
		parsed, err := ParseGrant(append(append([]byte(`{"capabilities":`), in.Capabilities...), '}'))
		if err != nil {
			return grant, FailField(CodeInvalidArgument, "capabilities", "capabilities must be a list of names or a map of capability to scope")
		}
		grant = parsed
	}
	for name, scope := range grant.Capabilities {
		spec, ok := LookupCapability(name)
		if !ok {
			return grant, FailField(CodeInvalidArgument, "capabilities."+name, "unknown capability")
		}
		if spec.Resource == ResourceServer && len(scope.Servers) == 0 && len(in.Servers) > 0 {
			scope.Servers = append([]int64(nil), in.Servers...)
		}
		if name == CapLANRequest && len(scope.Targets) == 0 && len(in.LANTargets) > 0 {
			scope.Targets = append([]string(nil), in.LANTargets...)
		}
		grant.Capabilities[name] = scope
	}
	if len(in.LANTargets) > 0 {
		if _, ok := grant.Capabilities[CapLANRequest]; !ok {
			return grant, FailField(CodeInvalidArgument, "lan_targets", "lan_targets requires the lan.request capability")
		}
	}
	return grant, nil
}
