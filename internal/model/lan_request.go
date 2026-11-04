package model

import (
	"net/netip"
	"strings"
)

// A plugin LAN request is one structured HTTP exchange with a single private
// address that the plugin manifest declared as a LAN target and the
// administrator granted for this server. It is separate from the
// probe_network_* diagnostics because it reaches the node's private networks,
// may change the target's state and returns a bounded response body. The
// Agent additionally requires its own local "lan" gate, which Controller can
// never raise.
const (
	AgentTaskTypeLANRequest   = "plugin_lan_request"
	AgentCapabilityLANRequest = "lan_request_v1"

	LANRequestMaxTimeoutMS           = 15000
	LANRequestMaxBodyBytes           = 16 << 10
	LANRequestMaxResponseBytes       = 16 << 10
	LANRequestDefaultResponseBytes   = 4 << 10
	LANRequestMaxHeaders             = 16
	LANRequestMaxHeaderValueBytes    = 1024
	LANRequestMaxResponseHeaders     = 32
	LANRequestMaxPathBytes           = 1024
	LANRequestMaxPathPrefixBytes     = 256
	LANRequestSchemeHTTP             = "http"
	LANRequestSchemeHTTPS            = "https"
	NetworkDiagnosticCodeLANDisabled = "agent_lan_gate_denied"
)

// LANRequestMethods are the methods a LAN target may declare.
var LANRequestMethods = map[string]bool{"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}

// LANRequestTaskPayload is signed by Controller. Address, port, scheme, path
// prefix and TLS mode are copied from the granted manifest target; only
// Method, Path, Headers and Body come from the plugin call, already checked
// against that target.
type LANRequestTaskPayload struct {
	NetworkDiagnosticEnvelope
	TargetID         string            `json:"target_id"`
	Scheme           string            `json:"scheme"`
	Address          string            `json:"address"`
	Port             int               `json:"port"`
	PathPrefix       string            `json:"path_prefix"`
	InsecureTLS      bool              `json:"insecure_tls,omitempty"`
	SideEffect       bool              `json:"side_effect,omitempty"`
	Method           string            `json:"method"`
	Path             string            `json:"path"`
	Headers          map[string]string `json:"headers,omitempty"`
	BodyBase64       string            `json:"body_base64,omitempty"`
	TimeoutMS        int               `json:"timeout_ms"`
	MaxResponseBytes int               `json:"max_response_bytes"`
}

type LANRequestResult struct {
	OperationID string            `json:"operation_id"`
	TargetID    string            `json:"target_id"`
	StatusCode  int               `json:"status_code,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	BodyBase64  string            `json:"body_base64,omitempty"`
	BodyBytes   int               `json:"body_bytes"`
	Truncated   bool              `json:"truncated"`
	DurationMS  float64           `json:"duration_ms"`
	ErrorClass  string            `json:"error_class,omitempty"`
}

var lanPrivatePrefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("fc00::/7"),
}

// LANAddressAllowed is the single address-class rule shared by Controller
// and Agent: RFC 1918, shared CGNAT space and IPv6 ULA only. Loopback,
// link-local (including cloud metadata), unspecified, multicast, broadcast,
// documentation and public addresses are never LAN targets.
func LANAddressAllowed(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" {
		return false
	}
	for _, prefix := range lanPrivatePrefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

// LANPathWithinPrefix reports whether a request path (optionally with a
// query) stays inside a declared prefix on a segment boundary: "/change"
// admits "/change", "/change/x" and "/change?a=1" but not "/changes".
func LANPathWithinPrefix(path, prefix string) bool {
	if prefix == "" || prefix == "/" {
		return strings.HasPrefix(path, "/")
	}
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	if strings.HasSuffix(prefix, "/") {
		return true
	}
	rest := path[len(prefix):]
	return rest == "" || rest[0] == '/' || rest[0] == '?'
}

// LANForbiddenRequestHeader lists headers the transport owns. Plugins can
// never set them.
func LANForbiddenRequestHeader(name string) bool {
	switch strings.ToLower(name) {
	case "host", "connection", "content-length", "transfer-encoding", "te", "trailer", "upgrade", "keep-alive", "expect", "proxy-authorization", "proxy-connection", "proxy-authenticate":
		return true
	}
	return strings.HasPrefix(strings.ToLower(name), "proxy-")
}
