package core

import (
	"math"
	"strings"

	"github.com/OboardProject/oboard/internal/model"
)

type LatencyGrade = model.LatencyGrade

const (
	LatencyGradeBasisRoute = model.LatencyGradeBasisRoute
	LatencyGradeBasisFixed = model.LatencyGradeBasisFixed
)

// FixedLatencyGrade is used when either end of a series has no known
// location: public anycast targets, custom addresses, and servers whose
// region is unresolved.
var FixedLatencyGrade = LatencyGrade{ExcellentMS: 80, FairMS: 180, Basis: LatencyGradeBasisFixed}

type geoAnchor struct {
	lat, lon float64
	// penalty is the typical extra round trip a route from this region pays
	// before it reaches a mainland international gateway: Eurasian land and
	// Indian Ocean routes are not as direct as the transpacific cables, and US
	// east coast traffic usually crosses to the west coast first.
	penalty float64
}

type provinceAnchor struct {
	code, name string
	lat, lon   float64
}

// Capital-city coordinates of the mainland provinces in the probe target
// table, keyed by both the resource's province code and its Chinese name.
var latencyProvinceAnchors = func() map[string]provinceAnchor {
	items := []provinceAnchor{
		{"bj", "北京", 39.90, 116.41}, {"tj", "天津", 39.13, 117.20}, {"he", "河北", 38.04, 114.51},
		{"sx", "山西", 37.87, 112.55}, {"nm", "内蒙古", 40.84, 111.75}, {"ln", "辽宁", 41.80, 123.43},
		{"jl", "吉林", 43.82, 125.32}, {"hl", "黑龙江", 45.80, 126.53}, {"sh", "上海", 31.23, 121.47},
		{"js", "江苏", 32.06, 118.80}, {"zj", "浙江", 30.27, 120.16}, {"ah", "安徽", 31.82, 117.23},
		{"fj", "福建", 26.07, 119.30}, {"jx", "江西", 28.68, 115.86}, {"sd", "山东", 36.65, 117.12},
		{"ha", "河南", 34.75, 113.62}, {"hb", "湖北", 30.59, 114.31}, {"hn", "湖南", 28.23, 112.94},
		{"gd", "广东", 23.13, 113.26}, {"gx", "广西", 22.82, 108.37}, {"hi", "海南", 20.04, 110.20},
		{"cq", "重庆", 29.56, 106.55}, {"sc", "四川", 30.57, 104.07}, {"gz", "贵州", 26.65, 106.63},
		{"yn", "云南", 25.04, 102.71}, {"xz", "西藏", 29.65, 91.17}, {"sn", "陕西", 34.34, 108.94},
		{"gs", "甘肃", 36.06, 103.83}, {"qh", "青海", 36.62, 101.78}, {"nx", "宁夏", 38.49, 106.23},
		{"xj", "新疆", 43.83, 87.62},
	}
	out := make(map[string]provinceAnchor, len(items)*2)
	for _, item := range items {
		out[item.code] = item
		out[item.name] = item
	}
	return out
}()

// Server regions are country-level, so each country is anchored at the city
// most servers facing mainland China are hosted in (for example the US west
// coast rather than the geographic centre).
var latencyCountryAnchors = map[string]geoAnchor{
	"CN": {30.59, 114.31, 0},
	"HK": {22.32, 114.17, 0}, "MO": {22.20, 113.55, 0}, "TW": {25.03, 121.56, 0},
	"JP": {35.68, 139.69, 0}, "KR": {37.57, 126.98, 0}, "MN": {47.89, 106.91, 20},
	"SG": {1.35, 103.82, 0}, "MY": {3.14, 101.69, 10}, "TH": {13.76, 100.50, 15},
	"VN": {21.03, 105.85, 15}, "PH": {14.60, 120.98, 15}, "ID": {-6.21, 106.85, 15},
	"KH": {11.56, 104.93, 20}, "MM": {16.87, 96.20, 30}, "LA": {17.98, 102.63, 25},
	"IN": {19.08, 72.88, 50}, "BD": {23.81, 90.41, 40}, "PK": {24.86, 67.01, 50},
	"NP": {27.72, 85.32, 40}, "LK": {6.93, 79.86, 40},
	"AU": {-33.87, 151.21, 15}, "NZ": {-36.85, 174.76, 20},
	"US": {34.05, -118.24, 0}, "CA": {49.28, -123.12, 10}, "MX": {19.43, -99.13, 30},
	"BR": {-23.55, -46.63, 40}, "AR": {-34.60, -58.38, 50}, "CL": {-33.45, -70.67, 40},
	"CO": {4.71, -74.07, 40}, "PE": {-12.05, -77.04, 40},
	"GB": {51.51, -0.13, 40}, "IE": {53.35, -6.26, 40}, "DE": {50.11, 8.68, 40},
	"FR": {48.86, 2.35, 40}, "NL": {52.37, 4.90, 40}, "BE": {50.85, 4.35, 40},
	"LU": {49.61, 6.13, 40}, "CH": {47.38, 8.54, 40}, "AT": {48.21, 16.37, 40},
	"IT": {45.46, 9.19, 40}, "ES": {40.42, -3.70, 40}, "PT": {38.72, -9.14, 40},
	"SE": {59.33, 18.07, 40}, "NO": {59.91, 10.75, 40}, "FI": {60.17, 24.94, 40},
	"DK": {55.68, 12.57, 40}, "PL": {52.23, 21.01, 40}, "CZ": {50.08, 14.44, 40},
	"HU": {47.50, 19.04, 40}, "RO": {44.43, 26.10, 40}, "BG": {42.70, 23.32, 40},
	"GR": {37.98, 23.73, 45}, "UA": {50.45, 30.52, 40}, "LT": {54.69, 25.28, 40},
	"LV": {56.95, 24.11, 40}, "EE": {59.44, 24.75, 40}, "RS": {44.79, 20.45, 40},
	"MD": {47.01, 28.86, 40}, "IS": {64.15, -21.94, 50},
	"RU": {55.76, 37.62, 30}, "KZ": {43.24, 76.89, 40}, "UZ": {41.30, 69.24, 50},
	"KG": {42.87, 74.59, 50}, "TR": {41.01, 28.98, 50},
	"AE": {25.20, 55.27, 40}, "SA": {24.71, 46.68, 50}, "IL": {32.08, 34.78, 50},
	"QA": {25.29, 51.53, 45}, "BH": {26.23, 50.59, 45}, "IR": {35.69, 51.39, 60},
	"EG": {30.04, 31.24, 50}, "ZA": {-26.20, 28.05, 60}, "NG": {6.52, 3.38, 70},
	"KE": {-1.29, 36.82, 60}, "MA": {33.57, -7.59, 50},
}

// RouteLatencyGrade derives a latency scale for a probe from a server in
// serverRegion (ISO 3166-1 alpha-2) to a mainland province (resource code or
// Chinese name). It models a good route as fibre propagation over the great
// circle (about 1 ms of round trip per 100 km) with a 10% path stretch, a
// fixed handling allowance, and the region's gateway detour. A cross-border
// route pays a larger fixed allowance than a domestic one.
//
// The resulting excellent ceiling is about 35 ms from Hong Kong to Guangdong,
// 65 ms from Tokyo, 165 ms from the US west coast and 180 ms from Frankfurt.
func RouteLatencyGrade(serverRegion, province string) (LatencyGrade, bool) {
	origin, ok := latencyCountryAnchors[NormalizeRegionCode(serverRegion)]
	if !ok {
		return FixedLatencyGrade, false
	}
	target, ok := latencyProvinceAnchors[strings.TrimSpace(province)]
	if !ok {
		return FixedLatencyGrade, false
	}
	base := 35.0
	if NormalizeRegionCode(serverRegion) == "CN" {
		base = 15
	}
	propagation := greatCircleKM(origin.lat, origin.lon, target.lat, target.lon) / 100
	excellent := base + propagation*1.1 + origin.penalty
	fair := excellent*1.5 + 20
	return LatencyGrade{ExcellentMS: roundTo5(excellent), FairMS: roundTo5(fair), Basis: LatencyGradeBasisRoute}, true
}

// LatencyTone classifies one latency sample against a grade.
func LatencyTone(g LatencyGrade, latencyMS float64) string {
	switch {
	case latencyMS <= float64(g.ExcellentMS):
		return "ok"
	case latencyMS <= float64(g.FairMS):
		return "fair"
	default:
		return "poor"
	}
}

func greatCircleKM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKM = 6371.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusKM * math.Asin(math.Min(1, math.Sqrt(a)))
}

func roundTo5(value float64) int {
	return int(math.Round(value/5) * 5)
}

// LatencyProvinceCode returns the probe-resource province code for a
// mainland province name or code, or "" when it is not a known province.
func LatencyProvinceCode(province string) string {
	if anchor, ok := latencyProvinceAnchors[strings.TrimSpace(province)]; ok {
		return anchor.code
	}
	return ""
}

// LatencyProbeTargetFamily reads the address family from a stable probe
// target ID, which ends in -v4 or -v6 by the resource contract.
func LatencyProbeTargetFamily(id string) string {
	switch {
	case strings.HasSuffix(id, "-v6"):
		return "ipv6"
	case strings.HasSuffix(id, "-v4"):
		return "ipv4"
	}
	return ""
}

// LatencyCarrierShortName drops the "中国" prefix of a carrier name for
// compact chart labels, so "中国移动" reads as "移动".
func LatencyCarrierShortName(carrier string) string {
	carrier = strings.TrimSpace(carrier)
	if short := strings.TrimPrefix(carrier, "中国"); short != "" {
		return short
	}
	return carrier
}
