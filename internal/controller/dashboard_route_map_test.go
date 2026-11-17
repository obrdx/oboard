package controller

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
)

func TestBuildDashboardRouteMapAggregatesRegionHops(t *testing.T) {
	id := func(v int64) *int64 { return &v }
	servers := []model.Server{
		{ID: 1, Name: "sha-secret-name", RegionMode: "manual", RegionCode: "CN", Status: model.ServerOnline, PublicIPv4: "203.0.113.1"},
		{ID: 2, Name: "hkg-a", RegionMode: "manual", RegionCode: "HK", Status: model.ServerOnline},
		{ID: 3, Name: "hkg-b", RegionMode: "manual", RegionCode: "HK", Status: model.ServerOffline},
		{ID: 4, Name: "lax", RegionMode: "manual", RegionCode: "US", Status: model.ServerOnline},
		{ID: 5, Name: "unknown", Status: model.ServerOnline},
	}
	inbounds := []model.Inbound{{ID: 10, ServerID: 1}, {ID: 11, ServerID: 1}, {ID: 12, ServerID: 2}}
	paths := []model.ProxyPath{
		{ID: 100, InboundID: 10, Enabled: true},
		{ID: 101, InboundID: 11, Enabled: true},
		{ID: 102, InboundID: 12, Enabled: true, EffectiveExitRegionCode: "JP"},
		{ID: 103, InboundID: 10, Enabled: false},
	}
	steps := []model.ProxyPathStep{
		{PathID: 100, Position: 2, ServerID: id(4)},
		{PathID: 100, Position: 1, ServerID: id(2)},
		{PathID: 101, Position: 1, ServerID: id(3)},
		{PathID: 101, Position: 2, ServerID: id(4)},
		{PathID: 102, Position: 1, ServerID: id(2)},
		{PathID: 102, Position: 2, NodeType: model.ProxyPathStepImported},
		{PathID: 103, Position: 1, ServerID: id(4)},
	}
	got := buildDashboardRouteMap(paths, steps, servers, inbounds)

	links := map[string]dashboardRouteLink{}
	for _, link := range got.Links {
		links[link.From+">"+link.To] = link
	}
	if len(links) != 3 {
		t.Fatalf("links = %#v", got.Links)
	}
	if link := links["CN>HK"]; link.Paths != 2 || link.DegradedPaths != 1 {
		t.Fatalf("CN>HK = %#v", link)
	}
	if link := links["HK>US"]; link.Paths != 2 || link.DegradedPaths != 1 {
		t.Fatalf("HK>US = %#v", link)
	}
	if link := links["HK>JP"]; link.Paths != 1 || link.DegradedPaths != 0 {
		t.Fatalf("HK>JP = %#v", link)
	}
	regions := map[string]dashboardRouteRegion{}
	for _, region := range got.Regions {
		regions[region.Code] = region
	}
	if r := regions["HK"]; r.Servers != 2 || r.Online != 1 || r.Paths != 3 {
		t.Fatalf("HK = %#v", r)
	}
	if r := regions["JP"]; r.Servers != 0 || r.Paths != 1 {
		t.Fatalf("JP = %#v", r)
	}
	if _, ok := regions[""]; ok {
		t.Fatalf("unknown region leaked: %#v", got.Regions)
	}
	encoded, _ := json.Marshal(got)
	for _, leak := range []string{"sha-secret-name", "203.0.113.1"} {
		if strings.Contains(string(encoded), leak) {
			t.Fatalf("route map leaked %q: %s", leak, encoded)
		}
	}
}
