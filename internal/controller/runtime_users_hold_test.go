package controller

import (
	"reflect"
	"testing"

	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
)

// A users package routed to an outbound the server has not deployed is held:
// the kernel would reject it and the lane would retry every redelivery period.
func TestMissingRuntimeUserRoutesComparesAgainstAppliedConfig(t *testing.T) {
	applied := `{"outbounds":[{"type":"direct","tag":"direct"},{"type":"socks","tag":"path-7-step-1"}],"endpoints":[{"type":"wireguard","tag":"warp-3"}]}`
	pkg := core.RuntimeUserPackage{Entries: []model.UsersInstallEntry{
		{InboundTag: "in-97", AuthUser: "a", RouteOutbound: "path-113-step-1"},
		{InboundTag: "in-97", AuthUser: "b", RouteOutbound: "path-113-step-1"},
		{InboundTag: "in-10", AuthUser: "c", RouteOutbound: "path-7-step-1"},
		{InboundTag: "in-11", AuthUser: "d", RouteOutbound: "warp-3"},
		{InboundTag: "in-12", AuthUser: "e", RouteOutbound: "direct"},
	}}
	if got := missingRuntimeUserRoutes(applied, pkg); !reflect.DeepEqual(got, []string{"path-113-step-1"}) {
		t.Fatalf("missing routes = %v", got)
	}
	pkg.Entries = pkg.Entries[2:]
	if got := missingRuntimeUserRoutes(applied, pkg); len(got) != 0 {
		t.Fatalf("deployed routes held: %v", got)
	}
	if got := missingRuntimeUserRoutes("", core.RuntimeUserPackage{Entries: []model.UsersInstallEntry{{RouteOutbound: "path-1-step-1"}}}); got != nil {
		t.Fatalf("server without a deployment was held: %v", got)
	}
}
