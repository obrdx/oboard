package core

import "github.com/OboardProject/oboard/internal/model"

// bootstrapResolverTag is the resolver that dial-side "bootstrap-primary"
// references are rewritten to: the failover group when present, the primary
// bootstrap resolver otherwise, and the configuration's own default resolver
// when the DNS block carries no bootstrap resolver at all (the local-only
// fallback after a benchmark found no usable candidate).
func bootstrapResolverTag(dns map[string]any) string {
	primary := false
	for _, item := range dnsServerItems(dns["servers"]) {
		switch item["tag"] {
		case bootstrapDNSGroupTag:
			return bootstrapDNSGroupTag
		case primaryBootstrapDNSTag:
			primary = true
		}
	}
	if primary {
		return primaryBootstrapDNSTag
	}
	return defaultTargetResolverTag(dns)
}

// Apply after detours and interface bindings are complete: only the physical
// dial owner is constrained by the host stack, not destinations inside a proxy.
func applyPhysicalDialResolvers(config *SingBoxConfig, server model.Server) {
	bootstrap := bootstrapResolverTag(config.DNS)
	for _, item := range config.Outbounds {
		defaultResolver := ""
		if item["type"] == "direct" {
			defaultResolver = defaultTargetResolverTag(config.DNS)
		}
		applyPhysicalDialResolver(item, server, bootstrap, defaultResolver)
	}
	for _, item := range config.Endpoints {
		applyPhysicalDialResolver(item, server, bootstrap, "")
	}
}

func applyPhysicalDialResolver(item map[string]any, server model.Server, bootstrap, defaultResolver string) {
	value := item["domain_resolver"]
	if value == nil {
		if defaultResolver == "" {
			return
		}
		value = defaultResolver
	}
	resolver := domainResolverMap(value)
	if resolver["server"] == primaryBootstrapDNSTag {
		resolver["server"] = bootstrap
	}
	// Explicit interface/source bindings may provide a family absent from the
	// host public-address inventory (for example an IPv6 tunnel).
	physical := true
	if item["type"] == "source-prefix" || item["type"] == "family-selector" {
		physical = false
	}
	for _, key := range []string{"detour", "bind_interface", "inet4_bind_address", "inet6_bind_address"} {
		if stringFromAny(item[key]) != "" {
			physical = false
		}
	}
	if physical {
		switch EffectiveIPStack(server) {
		case model.IPStackIPv4Only:
			resolver["strategy"] = "ipv4_only"
		case model.IPStackIPv6Only:
			resolver["strategy"] = "ipv6_only"
		}
	}
	if len(resolver) == 1 {
		item["domain_resolver"] = resolver["server"]
	} else {
		item["domain_resolver"] = resolver
	}
}
