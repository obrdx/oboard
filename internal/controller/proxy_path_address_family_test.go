package controller

import (
	"testing"

	"github.com/OboardProject/oboard/internal/model"
)

func TestProxyPathStepAddressFamilyNormalization(t *testing.T) {
	chain := &model.ProxyPath{Kind: model.ProxyPathKindChain}
	step := &model.ProxyPathStep{NodeType: model.ProxyPathStepServerInbound, ConfigJSON: `{"type":"ssh","ssh_port":2222,"address_family":"IPv6_Only"}`}
	family, err := takeProxyPathStepAddressFamily(step)
	if err != nil || family != "ipv6_only" || step.ConfigJSON != `{"ssh_port":2222,"type":"ssh"}` {
		t.Fatalf("take = %q %q err=%v", family, step.ConfigJSON, err)
	}
	if err := applyProxyPathStepAddressFamily(chain, step, family); err != nil || step.ConfigJSON != `{"address_family":"ipv6_only","ssh_port":2222,"type":"ssh"}` {
		t.Fatalf("apply = %q err=%v", step.ConfigJSON, err)
	}

	autoStep := &model.ProxyPathStep{NodeType: model.ProxyPathStepServerInbound, ConfigJSON: `{"address_family":"auto"}`}
	family, _ = takeProxyPathStepAddressFamily(autoStep)
	if err := applyProxyPathStepAddressFamily(chain, autoStep, family); err != nil || autoStep.ConfigJSON != `{}` {
		t.Fatalf("auto must be stored by omission: %q err=%v", autoStep.ConfigJSON, err)
	}

	if _, err := takeProxyPathStepAddressFamily(&model.ProxyPathStep{ConfigJSON: `{"address_family":"ipv5"}`}); err == nil {
		t.Fatal("unknown family must be rejected")
	}
	if err := applyProxyPathStepAddressFamily(chain, &model.ProxyPathStep{NodeType: model.ProxyPathStepWARP, ConfigJSON: `{}`}, "prefer_ipv6"); err == nil {
		t.Fatal("WARP step must reject a family")
	}
	branch := &model.ProxyPath{Kind: model.ProxyPathKindFamilyBranch}
	if err := applyProxyPathStepAddressFamily(branch, &model.ProxyPathStep{NodeType: model.ProxyPathStepServerInbound, ConfigJSON: `{}`}, "prefer_ipv6"); err == nil {
		t.Fatal("family branch step must reject a family")
	}
}
