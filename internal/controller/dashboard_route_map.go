package controller

import (
	"context"
	"sort"

	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
)

// dashboardRouteRegion is one country on the dashboard globe. Servers counts
// controlled servers located there; a region reached only as an imported,
// WARP or VPNGate exit has zero servers.
type dashboardRouteRegion struct {
	Code    string `json:"code"`
	Servers int    `json:"servers"`
	Online  int    `json:"online"`
	Paths   int    `json:"paths"`
}

// dashboardRouteLink is one directed region-to-region hop shared by Paths
// enabled proxy paths. DegradedPaths counts the paths whose controlled servers
// are not all online.
type dashboardRouteLink struct {
	From          string `json:"from"`
	To            string `json:"to"`
	Paths         int    `json:"paths"`
	DegradedPaths int    `json:"degraded_paths"`
}

type dashboardRouteMap struct {
	Regions []dashboardRouteRegion `json:"regions"`
	Links   []dashboardRouteLink   `json:"links"`
}

// dashboardRouteMap projects enabled proxy paths onto country-level hops for
// the dashboard globe. It carries region codes and counts only: no server
// names, addresses, ports, credentials or step configuration.
func (s *Server) dashboardRouteMap(ctx context.Context, servers []model.Server, inbounds []model.Inbound) (dashboardRouteMap, error) {
	paths, err := s.store.ListProxyPaths(ctx)
	if err != nil {
		return dashboardRouteMap{}, err
	}
	steps, err := s.store.ListProxyPathSteps(ctx)
	if err != nil {
		return dashboardRouteMap{}, err
	}
	externals, err := s.store.ListExternalOutbounds(ctx)
	if err != nil {
		return dashboardRouteMap{}, err
	}
	results, err := s.store.ListProxyPathEgressResults(ctx)
	if err != nil {
		return dashboardRouteMap{}, err
	}
	paths, _ = core.ResolveProxyPathExitRegions(paths, steps, servers, inbounds, externals, results)
	return buildDashboardRouteMap(paths, steps, servers, inbounds), nil
}

func dashboardRegionCode(code string) string {
	code = core.NormalizeRegionCode(code)
	if code == "AQ" {
		return ""
	}
	return code
}

func buildDashboardRouteMap(paths []model.ProxyPath, steps []model.ProxyPathStep, servers []model.Server, inbounds []model.Inbound) dashboardRouteMap {
	type serverInfo struct {
		region string
		online bool
	}
	serverByID := make(map[int64]serverInfo, len(servers))
	regions := map[string]*dashboardRouteRegion{}
	region := func(code string) *dashboardRouteRegion {
		item := regions[code]
		if item == nil {
			item = &dashboardRouteRegion{Code: code}
			regions[code] = item
		}
		return item
	}
	for _, server := range servers {
		code, _ := core.EffectiveServerRegion(server)
		info := serverInfo{region: dashboardRegionCode(code), online: server.Status == model.ServerOnline}
		serverByID[server.ID] = info
		if info.region == "" {
			continue
		}
		item := region(info.region)
		item.Servers++
		if info.online {
			item.Online++
		}
	}
	inboundServer := make(map[int64]int64, len(inbounds))
	for _, inbound := range inbounds {
		inboundServer[inbound.ID] = inbound.ServerID
	}
	stepsByPath := map[int64][]model.ProxyPathStep{}
	for _, step := range steps {
		stepsByPath[step.PathID] = append(stepsByPath[step.PathID], step)
	}

	type linkKey struct{ from, to string }
	links := map[linkKey]*dashboardRouteLink{}
	for _, path := range paths {
		if !path.Enabled {
			continue
		}
		hops := []string{}
		degraded := false
		visit := func(serverID int64) {
			info, ok := serverByID[serverID]
			if !ok {
				return
			}
			if !info.online {
				degraded = true
			}
			if info.region != "" {
				hops = append(hops, info.region)
			}
		}
		if serverID, ok := inboundServer[path.InboundID]; ok {
			visit(serverID)
		}
		ordered := append([]model.ProxyPathStep(nil), stepsByPath[path.ID]...)
		sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Position < ordered[j].Position })
		for _, step := range ordered {
			if step.ServerID != nil {
				visit(*step.ServerID)
			}
		}
		if exit := dashboardRegionCode(path.EffectiveExitRegionCode); exit != "" {
			hops = append(hops, exit)
		}
		seen := map[string]bool{}
		previous := ""
		for _, code := range hops {
			if code != previous && previous != "" {
				key := linkKey{previous, code}
				link := links[key]
				if link == nil {
					link = &dashboardRouteLink{From: previous, To: code}
					links[key] = link
				}
				link.Paths++
				if degraded {
					link.DegradedPaths++
				}
			}
			if !seen[code] {
				seen[code] = true
				region(code).Paths++
			}
			previous = code
		}
	}

	out := dashboardRouteMap{Regions: make([]dashboardRouteRegion, 0, len(regions)), Links: make([]dashboardRouteLink, 0, len(links))}
	for _, item := range regions {
		out.Regions = append(out.Regions, *item)
	}
	sort.Slice(out.Regions, func(i, j int) bool { return out.Regions[i].Code < out.Regions[j].Code })
	for _, link := range links {
		out.Links = append(out.Links, *link)
	}
	sort.Slice(out.Links, func(i, j int) bool {
		if out.Links[i].From != out.Links[j].From {
			return out.Links[i].From < out.Links[j].From
		}
		return out.Links[i].To < out.Links[j].To
	})
	return out
}
