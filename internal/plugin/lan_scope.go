package plugin

import (
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/OboardProject/oboard/internal/model"
)

var lanTargetIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// LANScope declares every private address lan.request may ever reach. Each
// target is one exact IP literal, port, scheme and path prefix; there are no
// ranges, hostnames or wildcards.
type LANScope struct {
	Targets []LANTarget `json:"targets"`
}

// LANTarget is one reviewable LAN endpoint. SideEffect marks targets that
// change state on the device; it is required for any method other than GET
// and HEAD and is shown to the administrator, audited on every call and
// confirmed before manual runs.
type LANTarget struct {
	ID          string   `json:"id"`
	Scheme      string   `json:"scheme"`
	Address     string   `json:"address"`
	Port        int      `json:"port"`
	Methods     []string `json:"methods"`
	PathPrefix  string   `json:"path_prefix"`
	SideEffect  bool     `json:"side_effect,omitempty"`
	InsecureTLS bool     `json:"insecure_tls,omitempty"`
	Reason      string   `json:"reason"`
}

// Endpoint is the human form shown in reviews and messages.
func (t LANTarget) Endpoint() string {
	host := t.Address
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return t.Scheme + "://" + host + ":" + strconv.Itoa(t.Port) + t.PathPrefix
}

func (t LANTarget) allowsMethod(method string) bool {
	for _, item := range t.Methods {
		if item == method {
			return true
		}
	}
	return false
}

// canonical is the identity a grant approves. Any change to it is a new
// target for permission review.
func (t LANTarget) canonical() string {
	return t.ID + "|" + t.Endpoint() + "|" + strings.Join(t.Methods, ",") + "|" + strconv.FormatBool(t.SideEffect) + "|" + strconv.FormatBool(t.InsecureTLS)
}

// LANTarget returns the declared target with id.
func (m Manifest) LANTarget(id string) (LANTarget, bool) {
	if m.LAN == nil {
		return LANTarget{}, false
	}
	for _, target := range m.LAN.Targets {
		if target.ID == id {
			return target, true
		}
	}
	return LANTarget{}, false
}

// SideEffectTargets lists declared targets marked side_effect.
func (m Manifest) SideEffectTargets() []string {
	out := []string{}
	if m.LAN != nil {
		for _, target := range m.LAN.Targets {
			if target.SideEffect {
				out = append(out, target.ID)
			}
		}
	}
	return out
}

func validateLANScope(m *Manifest) error {
	wants := m.HasCapability(CapLANRequest)
	if m.LAN == nil {
		if wants {
			return FailField(CodeInvalidManifest, "lan", "lan.request requires a lan target declaration")
		}
		return nil
	}
	if !wants {
		return FailField(CodeInvalidManifest, "lan", "lan is declared without the lan.request capability")
	}
	if len(m.LAN.Targets) == 0 || len(m.LAN.Targets) > maxLANTargets {
		return FailField(CodeInvalidManifest, "lan.targets", "declare between 1 and 16 LAN targets")
	}
	ids := map[string]bool{}
	for i := range m.LAN.Targets {
		target := &m.LAN.Targets[i]
		field := "lan.targets[" + strconv.Itoa(i) + "]"
		if !lanTargetIDPattern.MatchString(target.ID) || ids[target.ID] {
			return FailField(CodeInvalidManifest, field+".id", "id must be a unique lowercase identifier such as router_status")
		}
		ids[target.ID] = true
		target.Scheme = strings.ToLower(strings.TrimSpace(target.Scheme))
		if target.Scheme != model.LANRequestSchemeHTTP && target.Scheme != model.LANRequestSchemeHTTPS {
			return FailField(CodeInvalidManifest, field+".scheme", "scheme must be http or https")
		}
		if target.InsecureTLS && target.Scheme != model.LANRequestSchemeHTTPS {
			return FailField(CodeInvalidManifest, field+".insecure_tls", "insecure_tls is only valid for https targets")
		}
		ip, err := netip.ParseAddr(strings.TrimSpace(target.Address))
		if err != nil || ip.Zone() != "" {
			return FailField(CodeInvalidManifest, field+".address", "address must be an exact IP literal; hostnames and ranges are not accepted")
		}
		ip = ip.Unmap()
		if !model.LANAddressAllowed(ip) {
			return FailField(CodeInvalidManifest, field+".address", ip.String()+" is not a private LAN address (10/8, 172.16/12, 192.168/16, 100.64/10, fc00::/7); public hosts use http.request or network.http_probe")
		}
		target.Address = ip.String()
		if target.Port < 1 || target.Port > 65535 {
			return FailField(CodeInvalidManifest, field+".port", "port must be between 1 and 65535")
		}
		if err := validatePathPrefix(target.PathPrefix); err != nil {
			return FailField(CodeInvalidManifest, field+".path_prefix", err.Error())
		}
		if len(target.Methods) == 0 {
			return FailField(CodeInvalidManifest, field+".methods", "declare at least one method")
		}
		seen := map[string]bool{}
		mutating := false
		for j, method := range target.Methods {
			method = strings.ToUpper(strings.TrimSpace(method))
			if !model.LANRequestMethods[method] || seen[method] {
				return FailField(CodeInvalidManifest, field+".methods", "unsupported or duplicate method "+method)
			}
			seen[method] = true
			target.Methods[j] = method
			if method != "GET" && method != "HEAD" {
				mutating = true
			}
		}
		sort.Strings(target.Methods)
		if mutating && !target.SideEffect {
			return FailField(CodeInvalidManifest, field+".side_effect", "targets that allow methods other than GET and HEAD must set side_effect: true")
		}
		target.Reason = strings.TrimSpace(target.Reason)
		if target.Reason == "" || len(target.Reason) > 200 || hasControl(target.Reason, false) {
			return FailField(CodeInvalidManifest, field+".reason", "reason is required (at most 200 bytes) and is shown in the permission review")
		}
	}
	sort.Slice(m.LAN.Targets, func(i, j int) bool { return m.LAN.Targets[i].ID < m.LAN.Targets[j].ID })
	return nil
}

func validatePathPrefix(prefix string) error {
	if prefix == "" || prefix[0] != '/' || len(prefix) > model.LANRequestMaxPathPrefixBytes || strings.ContainsAny(prefix, "?#\\ \t\r\n%") {
		return errText("path_prefix must start with / and contain no query, fragment, whitespace, backslash or percent-encoding")
	}
	for _, segment := range strings.Split(prefix, "/") {
		if segment == "." || segment == ".." {
			return errText("path_prefix must not contain . or .. segments")
		}
	}
	return nil
}

// validateLANPath checks a call path (with optional query) against the
// target's prefix on a segment boundary, after rejecting dot segments in
// both raw and percent-decoded form.
func validateLANPath(path string, target LANTarget) error {
	if path == "" {
		path = target.PathPrefix
	}
	if len(path) > model.LANRequestMaxPathBytes || path[0] != '/' || strings.ContainsAny(path, "#\\ \t\r\n") {
		return Fail(CodeInvalidArgument, "path must start with / and contain no fragment, whitespace or backslash")
	}
	rawPath, _, _ := strings.Cut(path, "?")
	decoded, err := url.PathUnescape(rawPath)
	if err != nil {
		return Fail(CodeInvalidArgument, "path is not valid percent-encoding")
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." {
			return Fail(CodeTargetNotAllowed, "path "+path+" contains . or .. segments")
		}
	}
	if !model.LANPathWithinPrefix(path, target.PathPrefix) {
		return Fail(CodeTargetNotAllowed, "path "+path+" is outside LAN target "+target.ID+" (declared prefix "+target.PathPrefix+")")
	}
	return nil
}

type errText string

func (e errText) Error() string { return string(e) }
