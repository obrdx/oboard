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
	"sync/atomic"

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
	snellShapingSampleEnough             = 64
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
	if policy.PaddingStyle != "" && snellPaddingStyles[p.generator] != policy.PaddingStyle {
		return false
	}
	if policy.MaxPadding > 0 && p.padMax > policy.MaxPadding {
		return false
	}
	band := policy.FirstRecordDensity
	if band.IsZero() {
		return true
	}
	for _, length := range snellShapingPayloadLens {
		density := p.firstRecord(length).Density()
		inside := density >= band.Min && density <= band.Max
		if inside != (band.Mode == SnellDensityInside) {
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

var (
	snellShapingAcceptance      sync.Map
	snellShapingAcceptanceCount atomic.Int64
)

// SnellShapingAcceptance estimates the share of random PSKs a policy accepts
// from a fixed sample, so the verdict for a policy never changes.
func SnellShapingAcceptance(policy model.SnellShapingPolicy) float64 {
	if policy.IsZero() {
		return 1
	}
	if cached, ok := snellShapingAcceptance.Load(policy); ok {
		return cached.(float64)
	}
	hits, tried := 0, 0
	for tried < snellShapingSampleSize && hits < snellShapingSampleEnough {
		sum := sha256.Sum256([]byte("oboard-snell-shaping-sample-" + strconv.Itoa(tried)))
		if SnellPSKMeetsShaping(snellPSKFromEntropy(sum[:]), policy) {
			hits++
		}
		tried++
	}
	rate := float64(hits) / float64(tried)
	if count := snellShapingAcceptanceCount.Add(1); count > 1024 {
		snellShapingAcceptance.Clear()
		snellShapingAcceptanceCount.Store(0)
	}
	snellShapingAcceptance.Store(policy, rate)
	return rate
}

// ValidateSnellShapingFeasible rejects a policy too strict to select PSKs for.
func ValidateSnellShapingFeasible(policy model.SnellShapingPolicy) error {
	rate := SnellShapingAcceptance(policy)
	if snellShapingFeasibleRate(rate) {
		return nil
	}
	share := "抽样中没有 PSK 满足"
	if rate > 0 {
		share = fmt.Sprintf("只有约 %.3f%% 的 PSK 满足", rate*100)
	}
	return fmt.Errorf("%w: 整形策略过严，%s（至少需要 1/%d）。%s", ErrSnellShapingInfeasible, share, snellShapingMinAcceptanceDenominator, snellShapingRelaxation(policy))
}

func snellShapingFeasibleRate(rate float64) bool {
	return rate*snellShapingMinAcceptanceDenominator >= 1
}

// snellShapingRelaxation names the smallest change that makes a policy
// feasible. Only bit_biased padding moves the first record away from 0.5,
// and small padding caps leave too little padding to move it at all.
func snellShapingRelaxation(policy model.SnellShapingPolicy) string {
	feasible := func(p model.SnellShapingPolicy) bool { return snellShapingFeasibleRate(SnellShapingAcceptance(p)) }
	styles := []string{policy.PaddingStyle}
	if policy.PaddingStyle != "" && policy.PaddingStyle != SnellPaddingBitBiased {
		styles = append(styles, SnellPaddingBitBiased)
	}
	paddings := []int{policy.MaxPadding}
	if policy.MaxPadding > 0 {
		for _, limit := range []int{300, 400, 500, 600, SnellShapingMaxMaxPadding} {
			if limit > policy.MaxPadding {
				paddings = append(paddings, limit)
			}
		}
		paddings = append(paddings, 0)
	}
	for _, style := range styles {
		for _, padding := range paddings {
			if style == policy.PaddingStyle && padding == policy.MaxPadding {
				continue
			}
			candidate := policy
			candidate.PaddingStyle, candidate.MaxPadding = style, padding
			if !feasible(candidate) {
				continue
			}
			var changes []string
			if style != policy.PaddingStyle {
				changes = append(changes, "把填充风格改为「位偏置二进制」（其他风格的首包位密度都接近 0.5）")
			}
			if padding != policy.MaxPadding {
				if padding == 0 {
					changes = append(changes, "取消单记录填充上限")
				} else {
					changes = append(changes, fmt.Sprintf("把单记录填充上限调高到 %d", padding))
				}
			}
			if policy.FirstRecordDensity.IsZero() {
				return "可以" + strings.Join(changes, "，并") + "。"
			}
			return "可以" + strings.Join(changes, "，并") + "，或放宽首包位密度区间。"
		}
	}
	if policy.FirstRecordDensity.IsZero() {
		return "请放宽填充风格或单记录填充上限。"
	}
	return "请放宽或取消首包位密度区间。"
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
