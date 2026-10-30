package core

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/OboardProject/oboard/internal/model"
)

// Snell v6 padding generators, in kernel order.
const (
	SnellPaddingBitBiased = "bit_biased"
	SnellPaddingUTF8Like  = "utf8_like"
	SnellPaddingBCDLike   = "bcd_like"
	SnellPaddingMotif     = "motif"

	SnellDensityInside  = "inside"
	SnellDensityOutside = "outside"

	// pad_max is padMin (24-160) plus 160-960, capped at 730.
	SnellShapingMinMaxPadding = 184
	SnellShapingMaxMaxPadding = 730

	// A policy must accept at least one random PSK in this many, so selecting
	// one costs milliseconds and a feasible policy never exhausts the budget.
	snellShapingMinAcceptanceDenominator = 4000
	snellShapingSampleSize               = 20000
	snellShapingSelectBudget             = 1 << 20
)

var ErrSnellShapingInfeasible = errors.New("snell_shaping_infeasible")

var snellPaddingStyles = []string{SnellPaddingBitBiased, SnellPaddingUTF8Like, SnellPaddingBCDLike, SnellPaddingMotif}

// Request sizes the first record may carry: an empty reuse record, a bare
// CONNECT request, and requests that bring early application data up to the
// first-record cap.
var snellShapingPayloadLens = []int{0, 8, 20, 40, 70, 120, 200, 300, 400, 520, 768}

// SnellShapingPolicyOf returns the policy stored in an inbound or parameter
// set config. Malformed input yields the zero policy; validation rejects it.
func SnellShapingPolicyOf(extra map[string]any) model.SnellShapingPolicy {
	policy, err := parseSnellShapingPolicy(extra)
	if err != nil {
		return model.SnellShapingPolicy{}
	}
	return policy
}

// InboundSnellShaping is the policy every identity PSK on the inbound must meet.
func InboundSnellShaping(inbound model.Inbound) model.SnellShapingPolicy {
	if inbound.Protocol != model.ProtocolSnell {
		return model.SnellShapingPolicy{}
	}
	extra := parseExtra(inbound.ConfigJSON)
	if version, err := snellPanelVersion(extra); err != nil || version != SnellVersionV6 {
		return model.SnellShapingPolicy{}
	}
	if mode, err := normalizeSnellV6Mode(stringValue(extra, "mode", "default")); err != nil || mode != "default" {
		return model.SnellShapingPolicy{}
	}
	return SnellShapingPolicyOf(extra)
}

func parseSnellShapingPolicy(extra map[string]any) (model.SnellShapingPolicy, error) {
	raw, exists := extra["shaping"]
	if !exists || raw == nil {
		return model.SnellShapingPolicy{}, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return model.SnellShapingPolicy{}, err
	}
	return DecodeSnellShapingPolicy(encoded)
}

// DecodeSnellShapingPolicy strictly decodes and canonicalizes a policy.
func DecodeSnellShapingPolicy(encoded []byte) (model.SnellShapingPolicy, error) {
	var policy model.SnellShapingPolicy
	if len(bytes.TrimSpace(encoded)) == 0 || string(bytes.TrimSpace(encoded)) == "null" {
		return policy, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return model.SnellShapingPolicy{}, fmt.Errorf("snell shaping: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return model.SnellShapingPolicy{}, errors.New("snell shaping must be one object")
	}
	return NormalizeSnellShapingPolicy(policy)
}

// NormalizeSnellShapingPolicy validates the fields and rounds density bounds
// to three decimals so equal policies compare equal.
func NormalizeSnellShapingPolicy(policy model.SnellShapingPolicy) (model.SnellShapingPolicy, error) {
	if policy.PaddingStyle != "" && !stringSliceContains(snellPaddingStyles, policy.PaddingStyle) {
		return model.SnellShapingPolicy{}, fmt.Errorf("snell shaping padding_style must be one of %v", snellPaddingStyles)
	}
	if policy.MaxPadding != 0 && (policy.MaxPadding < SnellShapingMinMaxPadding || policy.MaxPadding > SnellShapingMaxMaxPadding) {
		return model.SnellShapingPolicy{}, fmt.Errorf("snell shaping max_padding must be 0 or %d-%d", SnellShapingMinMaxPadding, SnellShapingMaxMaxPadding)
	}
	band := policy.FirstRecordDensity
	if !band.IsZero() {
		if band.Mode != SnellDensityInside && band.Mode != SnellDensityOutside {
			return model.SnellShapingPolicy{}, errors.New("snell shaping first_record_density.mode must be inside or outside")
		}
		if math.IsNaN(band.Min) || math.IsNaN(band.Max) || band.Min < 0 || band.Max > 1 {
			return model.SnellShapingPolicy{}, errors.New("snell shaping first_record_density bounds must be within 0-1")
		}
		band.Min = math.Round(band.Min*1000) / 1000
		band.Max = math.Round(band.Max*1000) / 1000
		if band.Max-band.Min < 0.01 {
			return model.SnellShapingPolicy{}, errors.New("snell shaping first_record_density.max must exceed min by at least 0.01")
		}
		policy.FirstRecordDensity = band
	}
	return policy, nil
}

// SnellPSKMeetsShaping reports whether a PSK's derived first record satisfies
// the policy for every request size.
func SnellPSKMeetsShaping(psk string, policy model.SnellShapingPolicy) bool {
	if policy.IsZero() {
		return true
	}
	p := newSnellV6Profile([]byte(psk))
	return snellShapingMeets(p.generator, p.padMax, func(i int) float64 { return p.firstRecord(snellShapingPayloadLens[i]).Density() }, policy)
}

// snellShapingMeets is the one policy rule; density is computed lazily so a
// PSK that fails an early check costs no further first-record evaluation.
func snellShapingMeets(generator uint32, padMax int, density func(int) float64, policy model.SnellShapingPolicy) bool {
	if policy.PaddingStyle != "" && snellPaddingStyles[generator] != policy.PaddingStyle {
		return false
	}
	if policy.MaxPadding > 0 && padMax > policy.MaxPadding {
		return false
	}
	band := policy.FirstRecordDensity
	if band.IsZero() {
		return true
	}
	for i := range snellShapingPayloadLens {
		d := density(i)
		if (d >= band.Min && d <= band.Max) != (band.Mode == SnellDensityInside) {
			return false
		}
	}
	return true
}

func snellPSKFromEntropy(entropy []byte) string {
	return base64.RawURLEncoding.EncodeToString(entropy)
}

// NewShapedSnellPSK draws random PSKs in the regular credential format until
// one meets the policy. A policy that passed validation accepts at least one
// in 4000, so exhausting the budget means a broken invariant, not bad luck.
func NewShapedSnellPSK(policy model.SnellShapingPolicy) (string, error) {
	var entropy [32]byte
	for range snellShapingSelectBudget {
		if _, err := rand.Read(entropy[:]); err != nil {
			return "", err
		}
		psk := snellPSKFromEntropy(entropy[:])
		if SnellPSKMeetsShaping(psk, policy) {
			return psk, nil
		}
	}
	return "", fmt.Errorf("%w: no PSK met the policy within %d candidates", ErrSnellShapingInfeasible, snellShapingSelectBudget)
}

type snellShapingSample struct {
	generator uint32
	padMax    int
	densities [11]float64
}

// The fixed sample makes every verdict and recommendation deterministic and
// lets one policy be evaluated in well under a millisecond.
var snellShapingSamples = sync.OnceValue(func() []snellShapingSample {
	samples := make([]snellShapingSample, snellShapingSampleSize)
	for i := range samples {
		sum := sha256.Sum256([]byte("oboard-snell-shaping-sample-" + strconv.Itoa(i)))
		p := newSnellV6Profile([]byte(snellPSKFromEntropy(sum[:])))
		samples[i].generator, samples[i].padMax = p.generator, p.padMax
		for j, length := range snellShapingPayloadLens {
			samples[i].densities[j] = p.firstRecord(length).Density()
		}
	}
	return samples
})

// SnellShapingAcceptance is the share of the fixed PSK sample a policy accepts.
func SnellShapingAcceptance(policy model.SnellShapingPolicy) float64 {
	if policy.IsZero() {
		return 1
	}
	samples := snellShapingSamples()
	hits := 0
	for i := range samples {
		sample := &samples[i]
		if snellShapingMeets(sample.generator, sample.padMax, func(j int) float64 { return sample.densities[j] }, policy) {
			hits++
		}
	}
	return float64(hits) / float64(len(samples))
}

func snellShapingFeasibleRate(rate float64) bool {
	return rate*snellShapingMinAcceptanceDenominator >= 1
}

// SnellShapingCheck reports whether PSKs can be selected for a policy and,
// when they cannot, the closest policy that works.
type SnellShapingCheck struct {
	Feasible          bool                      `json:"feasible"`
	Acceptance        float64                   `json:"acceptance"`
	MinimumAcceptance float64                   `json:"minimum_acceptance"`
	Message           string                    `json:"message,omitempty"`
	Recommended       *model.SnellShapingPolicy `json:"recommended,omitempty"`
	Changes           []string                  `json:"changes,omitempty"`
}

// CheckSnellShaping validates a v6 default-mode policy and recommends the
// closest feasible one. The density goal is kept whenever the padding style or
// cap can be relaxed instead; the band is narrowed (outside) or widened
// (inside) only when that is not enough, and dropped last.
func CheckSnellShaping(policy model.SnellShapingPolicy) (SnellShapingCheck, error) {
	policy, err := NormalizeSnellShapingPolicy(policy)
	if err != nil {
		return SnellShapingCheck{}, err
	}
	rate := SnellShapingAcceptance(policy)
	check := SnellShapingCheck{Feasible: snellShapingFeasibleRate(rate), Acceptance: rate, MinimumAcceptance: 1.0 / snellShapingMinAcceptanceDenominator}
	if check.Feasible {
		return check, nil
	}
	share := "抽样中没有 PSK 满足"
	if rate > 0 {
		share = fmt.Sprintf("只有约 %.3f%% 的 PSK 满足", rate*100)
	}
	recommended, changes := recommendSnellShaping(policy)
	check.Recommended, check.Changes = &recommended, changes
	check.Message = fmt.Sprintf("当前组合过严，%s（至少需要 1/%d）。推荐：%s。", share, snellShapingMinAcceptanceDenominator, strings.Join(changes, "；"))
	return check, nil
}

func recommendSnellShaping(policy model.SnellShapingPolicy) (model.SnellShapingPolicy, []string) {
	styles := []string{policy.PaddingStyle}
	if policy.PaddingStyle != "" && policy.PaddingStyle != SnellPaddingBitBiased {
		styles = append(styles, SnellPaddingBitBiased)
	}
	if policy.PaddingStyle != "" {
		styles = append(styles, "")
	}
	paddings := []int{policy.MaxPadding}
	if policy.MaxPadding > 0 {
		for _, limit := range []int{300, 400, 500, 600} {
			if limit > policy.MaxPadding {
				paddings = append(paddings, limit)
			}
		}
		paddings = append(paddings, 0)
	}
	for _, band := range snellDensityRelaxations(policy.FirstRecordDensity) {
		for _, style := range styles {
			for _, padding := range paddings {
				candidate := model.SnellShapingPolicy{PaddingStyle: style, MaxPadding: padding, FirstRecordDensity: band}
				if candidate != policy && snellShapingFeasibleRate(SnellShapingAcceptance(candidate)) {
					return candidate, snellShapingChanges(policy, candidate)
				}
			}
		}
	}
	return model.SnellShapingPolicy{}, []string{"取消全部整形约束"}
}

// snellDensityRelaxations keeps the band first, then moves its edges 0.01 at a
// time: an outside band shrinks toward its centre, an inside band grows.
func snellDensityRelaxations(band model.SnellDensityBand) []model.SnellDensityBand {
	out := []model.SnellDensityBand{band}
	if band.IsZero() {
		return out
	}
	round := func(v float64) float64 { return math.Round(v*1000) / 1000 }
	for step := 1; step <= 100; step++ {
		delta := float64(step) / 100
		next := band
		if band.Mode == SnellDensityOutside {
			next.Min, next.Max = round(band.Min+delta), round(band.Max-delta)
			if next.Max-next.Min < 0.01 {
				break
			}
		} else {
			next.Min, next.Max = round(math.Max(0, band.Min-delta)), round(math.Min(1, band.Max+delta))
			if next == out[len(out)-1] {
				break
			}
		}
		out = append(out, next)
	}
	return append(out, model.SnellDensityBand{})
}

func snellShapingChanges(from, to model.SnellShapingPolicy) []string {
	var changes []string
	if from.FirstRecordDensity != to.FirstRecordDensity {
		if to.FirstRecordDensity.IsZero() {
			changes = append(changes, "取消首包位密度约束")
		} else {
			verb := "避开"
			if to.FirstRecordDensity.Mode == SnellDensityInside {
				verb = "限定在"
			}
			changes = append(changes, fmt.Sprintf("首包位密度区间改为%s %g–%g", verb, to.FirstRecordDensity.Min, to.FirstRecordDensity.Max))
		}
	}
	if from.PaddingStyle != to.PaddingStyle {
		switch {
		case to.PaddingStyle == "":
			changes = append(changes, "填充风格改为「不限制」")
		case !to.FirstRecordDensity.IsZero():
			changes = append(changes, "填充风格改为「位偏置二进制」（其他风格的首包位密度都接近 0.5）")
		default:
			changes = append(changes, "填充风格改为「位偏置二进制」")
		}
	}
	if from.MaxPadding != to.MaxPadding {
		if to.MaxPadding == 0 {
			changes = append(changes, "取消单记录填充上限")
		} else {
			changes = append(changes, fmt.Sprintf("单记录填充上限调高到 %d", to.MaxPadding))
		}
	}
	return changes
}

// ValidateSnellShapingFeasible rejects a policy too strict to select PSKs for
// and names the recommended change.
func ValidateSnellShapingFeasible(policy model.SnellShapingPolicy) error {
	check, err := CheckSnellShaping(policy)
	if err != nil {
		return err
	}
	if check.Feasible {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrSnellShapingInfeasible, check.Message)
}

// ValidateSnellShaping checks a policy against the Snell version and mode it
// is attached to, then checks that PSKs can be selected for it.
func ValidateSnellShaping(version int, mode string, policy model.SnellShapingPolicy) error {
	if policy.IsZero() {
		return nil
	}
	policy, err := NormalizeSnellShapingPolicy(policy)
	if err != nil {
		return err
	}
	if normalized, err := normalizeSnellV6Mode(mode); err != nil || version != SnellVersionV6 || normalized != "default" {
		return errors.New("snell shaping requires version 6 with mode default")
	}
	return ValidateSnellShapingFeasible(policy)
}

func validateSnellShaping(extra map[string]any, version int, side transportSide) error {
	if _, exists := extra["shaping"]; !exists {
		return nil
	}
	if side != transportSideInbound {
		return errors.New("snell shaping applies only to managed inbounds")
	}
	policy, err := parseSnellShapingPolicy(extra)
	if err != nil {
		return err
	}
	return ValidateSnellShaping(version, stringValue(extra, "mode", "default"), policy)
}
