package core

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
)

var snellAvoidBand = model.SnellShapingPolicy{FirstRecordDensity: model.SnellDensityBand{Mode: SnellDensityOutside, Min: 0.36, Max: 0.65}}

func snellShapingInbound(config map[string]any) model.Inbound {
	raw, _ := json.Marshal(config)
	return model.Inbound{ID: 1, Protocol: model.ProtocolSnell, ListenIP: "0.0.0.0", Port: 8443, Enabled: true, ConfigJSON: string(raw)}
}

func TestSnellShapingValidation(t *testing.T) {
	band := map[string]any{"first_record_density": map[string]any{"mode": "outside", "min": 0.36, "max": 0.65}}
	cases := []struct {
		name   string
		config map[string]any
		want   string
	}{
		{"valid", map[string]any{"version": 6, "shaping": band}, ""},
		{"empty", map[string]any{"version": 6, "shaping": map[string]any{}}, ""},
		{"v4", map[string]any{"version": 4, "shaping": band}, "requires version 6"},
		{"unshaped", map[string]any{"version": 6, "mode": "unshaped", "shaping": band}, "requires version 6"},
		{"unknown field", map[string]any{"version": 6, "shaping": map[string]any{"seed": 1}}, "unknown field"},
		{"style", map[string]any{"version": 6, "shaping": map[string]any{"padding_style": "ascii"}}, "padding_style"},
		{"max padding", map[string]any{"version": 6, "shaping": map[string]any{"max_padding": 100}}, "max_padding"},
		{"density mode", map[string]any{"version": 6, "shaping": map[string]any{"first_record_density": map[string]any{"mode": "above", "min": 0.6, "max": 1}}}, "inside or outside"},
		{"narrow band", map[string]any{"version": 6, "shaping": map[string]any{"first_record_density": map[string]any{"mode": "inside", "min": 0.6, "max": 0.605}}}, "at least 0.01"},
		{"infeasible", map[string]any{"version": 6, "shaping": map[string]any{"padding_style": "utf8_like", "first_record_density": map[string]any{"mode": "outside", "min": 0.425, "max": 0.573}}}, "snell_shaping_infeasible"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := snellAdapter{}.ValidateInbound(snellShapingInbound(tc.config))
			if tc.want == "" && err != nil {
				t.Fatal(err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("err=%v, want %q", err, tc.want)
			}
		})
	}
	outbound := model.Outbound{TargetAddress: "198.51.100.1", TargetPort: 443, ConfigJSON: `{"version":6,"psk":"remote-server-psk","shaping":{"padding_style":"motif"}}`}
	if err := (snellAdapter{}).ValidateOutbound(outbound); err == nil || !strings.Contains(err.Error(), "managed inbounds") {
		t.Fatalf("outbound shaping accepted: %v", err)
	}
	if !errors.Is(ValidateSnellShaping(6, "default", model.SnellShapingPolicy{PaddingStyle: SnellPaddingUTF8Like, FirstRecordDensity: model.SnellDensityBand{Mode: SnellDensityOutside, Min: 0.425, Max: 0.573}}), ErrSnellShapingInfeasible) {
		t.Fatal("infeasible policy is not ErrSnellShapingInfeasible")
	}
}

// Densities come from the kernel vectors: TestPSK-gamma-3333 stays within
// 0.506-0.522 and kiWmPJ6YYnviquK-dIQ2Sv8M within 0.620-0.726.
func TestSnellPSKMeetsShaping(t *testing.T) {
	carrierDrop := model.SnellShapingPolicy{FirstRecordDensity: model.SnellDensityBand{Mode: SnellDensityOutside, Min: 0.425, Max: 0.573}}
	if SnellPSKMeetsShaping("TestPSK-gamma-3333", carrierDrop) {
		t.Fatal("a PSK inside the drop zone passed")
	}
	if !SnellPSKMeetsShaping("kiWmPJ6YYnviquK-dIQ2Sv8M", carrierDrop) {
		t.Fatal("a PSK clear of the drop zone failed")
	}
	if SnellPSKMeetsShaping("kiWmPJ6YYnviquK-dIQ2Sv8M", model.SnellShapingPolicy{FirstRecordDensity: model.SnellDensityBand{Mode: SnellDensityInside, Min: 0.65, Max: 1}}) {
		t.Fatal("inside band must hold for every request size")
	}
	if !SnellPSKMeetsShaping("TestPSK-gamma-3333", model.SnellShapingPolicy{PaddingStyle: SnellPaddingUTF8Like}) || SnellPSKMeetsShaping("TestPSK-gamma-3333", model.SnellShapingPolicy{PaddingStyle: SnellPaddingBitBiased}) {
		t.Fatal("padding style does not follow the derived generator")
	}
	if !SnellPSKMeetsShaping("TestPSK-gamma-3333", model.SnellShapingPolicy{MaxPadding: 694}) || SnellPSKMeetsShaping("TestPSK-gamma-3333", model.SnellShapingPolicy{MaxPadding: 693}) {
		t.Fatal("max_padding does not bound the derived pad_max")
	}
}

func TestNewShapedSnellPSK(t *testing.T) {
	seen := map[string]bool{}
	for range 16 {
		psk, err := NewShapedSnellPSK(snellAvoidBand)
		if err != nil {
			t.Fatal(err)
		}
		if len(psk) != 43 || seen[psk] || !SnellPSKMeetsShaping(psk, snellAvoidBand) {
			t.Fatalf("bad shaped PSK %q", psk)
		}
		if err := validateSnellPSKLength(psk, SnellVersionV6); err != nil {
			t.Fatal(err)
		}
		seen[psk] = true
	}
}

func TestSnellShapingNormalizationIsCanonical(t *testing.T) {
	a, err := DecodeSnellShapingPolicy([]byte(`{"first_record_density":{"mode":"outside","min":0.36000001,"max":0.65}}`))
	if err != nil {
		t.Fatal(err)
	}
	if a != snellAvoidBand {
		t.Fatalf("normalized policy %+v", a)
	}
	if _, err := DecodeSnellShapingPolicy([]byte(`{"padding_style":"motif"} {}`)); err == nil {
		t.Fatal("trailing data accepted")
	}
}

func TestProxyCredentialScopesCarrySnellShaping(t *testing.T) {
	user := model.User{ID: 1, Username: "account", Status: "active"}
	shaped := snellShapingInbound(map[string]any{"version": 6, "shaping": map[string]any{"first_record_density": map[string]any{"mode": "outside", "min": 0.36, "max": 0.65}}})
	unshaped := snellShapingInbound(map[string]any{"version": 6, "mode": "unshaped", "shaping": map[string]any{"padding_style": "motif"}})
	unshaped.ID = 2
	v4 := snellShapingInbound(map[string]any{"version": 4, "shaping": map[string]any{"padding_style": "motif"}})
	v4.ID = 3
	bindings := []model.InboundUser{{InboundID: 1, UserID: 1, Enabled: true}, {InboundID: 2, UserID: 1, Enabled: true}, {InboundID: 3, UserID: 1, Enabled: true}}
	scopes := ProxyCredentialScopes([]model.User{user}, []model.Inbound{shaped, unshaped, v4}, ConfigOptions{InboundUsers: bindings})
	if len(scopes) != 3 {
		t.Fatalf("scopes=%d", len(scopes))
	}
	for _, scope := range scopes {
		want := model.SnellShapingPolicy{}
		if scope.InboundID == 1 {
			want = snellAvoidBand
		}
		if scope.SnellShaping != want {
			t.Fatalf("inbound %d carries %+v", scope.InboundID, scope.SnellShaping)
		}
	}
}

// Only bit_biased padding moves the first record away from 0.5, and a small
// padding cap leaves too little padding to move it; the error names the fix.
func TestSnellShapingInfeasibleNamesTheFix(t *testing.T) {
	cases := []struct {
		policy      model.SnellShapingPolicy
		want, avoid []string
	}{
		{model.SnellShapingPolicy{PaddingStyle: SnellPaddingUTF8Like, FirstRecordDensity: snellAvoidBand.FirstRecordDensity}, []string{"位偏置二进制", "放宽首包位密度区间"}, []string{"填充上限"}},
		{model.SnellShapingPolicy{MaxPadding: 250, FirstRecordDensity: snellAvoidBand.FirstRecordDensity}, []string{"调高到 400"}, []string{"位偏置二进制"}},
		{model.SnellShapingPolicy{PaddingStyle: SnellPaddingBitBiased, MaxPadding: 184}, []string{"调高到 300"}, []string{"密度区间"}},
	}
	for _, tc := range cases {
		err := ValidateSnellShapingFeasible(tc.policy)
		if !errors.Is(err, ErrSnellShapingInfeasible) {
			t.Fatalf("%+v: err=%v", tc.policy, err)
		}
		for _, want := range tc.want {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%+v: %q lacks %q", tc.policy, err, want)
			}
		}
		for _, avoid := range tc.avoid {
			if strings.Contains(err.Error(), avoid) {
				t.Fatalf("%+v: %q should not mention %q", tc.policy, err, avoid)
			}
		}
	}
}
