package core

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/OboardProject/oboard/internal/model"
)

func ExternalOutboundFingerprint(v model.ExternalOutbound) string {
	var config any
	_ = json.Unmarshal([]byte(v.ConfigJSON), &config)
	raw, _ := json.Marshal([]any{v.ID, v.Protocol, v.Scope, v.ServerID, v.TargetAddress, v.TargetPort, config})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func ExternalOutboundProbeConfig(v model.ExternalOutbound) (json.RawMessage, error) {
	item, err := externalOutboundToSingBoxWithTag(v, model.Server{}, nil, "external-probe")
	if err != nil {
		return nil, err
	}
	for _, field := range []string{"detour", "bind_interface", "inet4_bind_address", "inet6_bind_address", "routing_mark", "domain_resolver", "network_strategy", "network_type", "fallback_network_type", "fallback_delay"} {
		delete(item, field)
	}
	return json.Marshal(item)
}

func ExternalOutboundProbeCurrent(v model.ExternalOutbound) bool {
	return v.ProbeFingerprint != "" && v.ProbeFingerprint == ExternalOutboundFingerprint(v)
}
