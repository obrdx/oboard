package model

// SnellShapingPolicy constrains the Snell v6 default-mode traffic profile. The
// profile is derived from the PSK on both client and server, so the policy is
// enforced by choosing each identity's PSK; it never reaches the kernel.
type SnellShapingPolicy struct {
	PaddingStyle       string           `json:"padding_style,omitempty"`
	MaxPadding         int              `json:"max_padding,omitempty"`
	FirstRecordDensity SnellDensityBand `json:"first_record_density,omitzero"`
}

// SnellDensityBand bounds the expected share of one bits in the first record
// for every request size: "inside" keeps it within [Min, Max], "outside"
// keeps it out of that band.
type SnellDensityBand struct {
	Mode string  `json:"mode"`
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
}

func (b SnellDensityBand) IsZero() bool { return b == SnellDensityBand{} }

func (p SnellShapingPolicy) IsZero() bool { return p == SnellShapingPolicy{} }
