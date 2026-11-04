package plugin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/OboardProject/oboard/internal/model"
)

type lanArgs struct {
	ServerID         string            `json:"server_id"`
	Target           string            `json:"target"`
	Method           string            `json:"method"`
	Path             string            `json:"path"`
	Headers          map[string]string `json:"headers"`
	Body             *string           `json:"body"`
	BodyEncoding     string            `json:"body_encoding"`
	TimeoutMS        *int              `json:"timeout_ms"`
	MaxResponseBytes *int              `json:"max_response_bytes"`
}

// lanTarget resolves a target the run may use: declared by the published
// manifest, approved by the grant and, for a draft trial run, declared
// identically by the draft.
func lanTargetFor(call callContext, id string) (LANTarget, error) {
	target, ok := call.loaded.manifest.LANTarget(id)
	if !ok {
		return LANTarget{}, Fail(CodeTargetNotAllowed, "LAN target "+id+" is not declared in the published manifest")
	}
	if call.draft != nil {
		drafted, ok := call.draft.LANTarget(id)
		if !ok || drafted.canonical() != target.canonical() {
			return LANTarget{}, Fail(CodeTargetNotAllowed, "LAN target "+id+" differs between the draft and the published version; publish and review it first")
		}
		if target.SideEffect {
			return LANTarget{}, Fail(CodeDraftSideEffectBlocked, "LAN target "+id+" is marked side_effect and is blocked in a draft trial run; publish the draft to use it")
		}
	}
	if !call.grant.AllowsLANTarget(id) {
		return LANTarget{}, Fail(CodeTargetNotAllowed, "LAN target "+id+" ("+target.Endpoint()+") is not approved in the administrator grant")
	}
	return target, nil
}

// handleLAN turns one lan.request call into one signed plugin_lan_request
// Agent task. Address, port, scheme, path prefix and TLS mode always come
// from the granted manifest target, never from the call.
func (s *Service) handleLAN(ctx context.Context, call callContext, raw json.RawMessage) (any, string, map[string]any, error) {
	var args lanArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, "", nil, err
	}
	serverID, err := parseServerArg(args.ServerID)
	if err != nil {
		return nil, "", nil, err
	}
	resource := "server:" + args.ServerID
	detail := map[string]any{"target": args.Target}
	target, err := lanTargetFor(call, strings.TrimSpace(args.Target))
	if err != nil {
		return nil, resource, detail, err
	}
	method := strings.ToUpper(strings.TrimSpace(args.Method))
	if method == "" {
		method = "GET"
	}
	detail["method"], detail["endpoint"], detail["side_effect"] = method, target.Endpoint(), target.SideEffect
	if !target.allowsMethod(method) {
		return nil, resource, detail, Fail(CodeTargetNotAllowed, "method "+method+" is not declared for LAN target "+target.ID+" (allowed: "+strings.Join(target.Methods, ", ")+")")
	}
	path := args.Path
	if path == "" {
		path = target.PathPrefix
	}
	if err := validateLANPath(path, target); err != nil {
		return nil, resource, detail, err
	}
	detail["path"] = path
	if len(args.Headers) > model.LANRequestMaxHeaders {
		return nil, resource, detail, Fail(CodeInvalidArgument, "at most 16 headers")
	}
	for name, value := range args.Headers {
		if !validHeaderToken(name) || model.LANForbiddenRequestHeader(name) {
			return nil, resource, detail, Fail(CodeInvalidArgument, "header "+name+" cannot be set by a plugin")
		}
		if len(value) > model.LANRequestMaxHeaderValueBytes || strings.ContainsAny(value, "\r\n\x00") {
			return nil, resource, detail, Fail(CodeInvalidArgument, "header "+name+" value is invalid")
		}
	}
	var body []byte
	if args.Body != nil {
		switch args.BodyEncoding {
		case "", "utf8":
			body = []byte(*args.Body)
		case "base64":
			if body, err = base64.StdEncoding.DecodeString(*args.Body); err != nil {
				return nil, resource, detail, Fail(CodeInvalidArgument, "body is not base64")
			}
		default:
			return nil, resource, detail, Fail(CodeInvalidArgument, "body_encoding must be utf8 or base64")
		}
		if method == "GET" || method == "HEAD" {
			return nil, resource, detail, Fail(CodeInvalidArgument, "GET and HEAD requests cannot carry a body")
		}
	}
	if len(body) > model.LANRequestMaxBodyBytes {
		return nil, resource, detail, Fail(CodeInvalidArgument, "body exceeds 16 KiB")
	}
	timeout, err := intDefault(args.TimeoutMS, 5000, 500, model.LANRequestMaxTimeoutMS, "timeout_ms")
	if err != nil {
		return nil, resource, detail, err
	}
	maxResponse, err := intDefault(args.MaxResponseBytes, model.LANRequestDefaultResponseBytes, 1, model.LANRequestMaxResponseBytes, "max_response_bytes")
	if err != nil {
		return nil, resource, detail, err
	}
	server, err := s.serverInScope(ctx, call, CapLANRequest, serverID)
	if err != nil {
		return nil, resource, detail, err
	}
	if !server.Online {
		return nil, resource, detail, Fail(CodeServerOffline, "server "+args.ServerID+" is offline; retry on a later run")
	}
	if !hasString(server.Capabilities, model.AgentCapabilityLANRequest) {
		return nil, resource, detail, Fail(CodeUnsupportedCapability, "the Agent on server "+args.ServerID+" does not support lan.request; update the Agent")
	}
	if !server.PluginsGate {
		return nil, resource, detail, Fail(CodeAgentPolicyDenied, "server "+args.ServerID+" does not allow plugin operations locally (obag remote-access allow plugins)")
	}
	if !server.LANGate {
		return nil, resource, detail, Fail(CodeAgentPolicyDenied, "server "+args.ServerID+" does not allow plugin LAN requests locally (run obag remote-access allow lan on the node)")
	}
	limits, _ := EffectiveLimits(call.loaded.manifest.Limits, s.Settings(ctx).MaxTimeout)
	allowed, err := s.store.ConsumePluginRunBudget(ctx, call.run.UUID, call.run.LeaseGeneration, "agent", limits.AgentOperations)
	if err != nil {
		return nil, resource, detail, Fail(CodeInternal, "internal error")
	}
	if !allowed {
		return nil, resource, detail, Fail(CodeLimitExceeded, "Agent operation budget for this run is exhausted")
	}
	now := s.now()
	envelope := model.NetworkDiagnosticEnvelope{
		ProtocolVersion: model.NetworkDiagnosticProtocolVersion, OperationID: "pop_" + randomToken(12), Origin: model.NetworkDiagnosticOriginPlugin,
		RunID: call.run.UUID, PluginID: call.run.PluginKey, InstanceID: call.instance.ID, IssuedAt: now, ExpiresAt: now.Add(agentStartWindow),
	}
	payload := model.LANRequestTaskPayload{
		NetworkDiagnosticEnvelope: envelope, TargetID: target.ID, Scheme: target.Scheme, Address: target.Address, Port: target.Port,
		PathPrefix: target.PathPrefix, InsecureTLS: target.InsecureTLS, SideEffect: target.SideEffect, Method: method, Path: path,
		Headers: args.Headers, TimeoutMS: timeout, MaxResponseBytes: maxResponse,
	}
	if len(body) > 0 {
		payload.BodyBase64 = base64.StdEncoding.EncodeToString(body)
	}
	deadline, _ := ctx.Deadline()
	raw, err = s.host.RunNetworkDiagnostic(ctx, DiagnosticRequest{Capability: CapLANRequest, ServerID: serverID, RunUUID: call.run.UUID, PluginKey: call.run.PluginKey, InstanceID: call.instance.ID, Payload: mustMarshal(payload), Deadline: deadline})
	if err != nil {
		return nil, resource, detail, err
	}
	var result model.LANRequestResult
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if len(raw) > 64<<10 || dec.Decode(&result) != nil || result.OperationID != envelope.OperationID || result.TargetID != target.ID || len(result.Headers) > model.LANRequestMaxResponseHeaders {
		return nil, resource, detail, Fail(CodeOperationFailed, "the Agent returned an invalid LAN request result")
	}
	decoded, err := base64.StdEncoding.DecodeString(result.BodyBase64)
	if err != nil || len(decoded) > maxResponse {
		return nil, resource, detail, Fail(CodeOperationFailed, "the Agent returned an invalid LAN response body")
	}
	detail["status"], detail["bytes"] = result.StatusCode, len(decoded)
	if result.ErrorClass != "" {
		detail["error_class"] = result.ErrorClass
	}
	out := map[string]any{
		"target": target.ID, "status": result.StatusCode, "headers": result.Headers, "truncated": result.Truncated,
		"duration_ms": result.DurationMS, "side_effect": target.SideEffect,
	}
	if out["headers"] == nil {
		out["headers"] = map[string]string{}
	}
	if result.ErrorClass != "" {
		out["error_class"] = result.ErrorClass
	}
	if utf8.Valid(decoded) {
		out["body"] = string(decoded)
		if strings.Contains(result.Headers["content-type"], "json") && !result.Truncated {
			var value any
			if json.Unmarshal(decoded, &value) == nil {
				out["json"] = value
			}
		}
	} else {
		out["body_base64"] = result.BodyBase64
	}
	return out, resource, detail, nil
}

func validHeaderToken(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	return true
}
