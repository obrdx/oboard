package core

import "testing"

func TestRouteLatencyGradeScalesWithServerRegion(t *testing.T) {
	cases := []struct {
		region, province string
		minExcellent     int
		maxExcellent     int
	}{
		{"HK", "广东", 30, 40},
		{"JP", "gd", 55, 75},
		{"US", "广东", 150, 175},
		{"DE", "广东", 165, 195},
		{"CN", "广东", 15, 35},
	}
	for _, tc := range cases {
		grade, ok := RouteLatencyGrade(tc.region, tc.province)
		if !ok || grade.Basis != LatencyGradeBasisRoute {
			t.Fatalf("%s→%s: expected a route grade, got %+v", tc.region, tc.province, grade)
		}
		if grade.ExcellentMS < tc.minExcellent || grade.ExcellentMS > tc.maxExcellent || grade.FairMS <= grade.ExcellentMS {
			t.Fatalf("%s→%s: excellent=%d fair=%d outside [%d,%d]", tc.region, tc.province, grade.ExcellentMS, grade.FairMS, tc.minExcellent, tc.maxExcellent)
		}
	}
	us, _ := RouteLatencyGrade("US", "广东")
	if LatencyTone(us, 165) != "ok" {
		t.Fatalf("165 ms from the US to Guangdong should be excellent, got %s (%+v)", LatencyTone(us, 165), us)
	}
	hk, _ := RouteLatencyGrade("HK", "广东")
	if LatencyTone(hk, 80) == "ok" {
		t.Fatalf("80 ms from Hong Kong to Guangdong must not be excellent (%+v)", hk)
	}
}

func TestRouteLatencyGradeFallsBackWhenAnEndIsUnknown(t *testing.T) {
	for _, tc := range [][2]string{{"", "广东"}, {"ZZ", "广东"}, {"US", ""}, {"US", "火星"}} {
		grade, ok := RouteLatencyGrade(tc[0], tc[1])
		if ok || grade != FixedLatencyGrade {
			t.Fatalf("%v: expected fixed grade, got %+v", tc, grade)
		}
	}
}
