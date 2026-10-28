package core

import (
	"encoding/json"
	"os"
	"testing"
)

// The kernel copy of sing-snell verifies the same file in
// snellv6/oboard_shape_vectors_test.go; regenerate both together.
func TestSnellV6ProfileMatchesKernelVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/snell_v6_shape_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		PSK            string               `json:"psk"`
		Generator      uint32               `json:"generator"`
		PadMax         int                  `json:"pad_max"`
		FirstRecordCap int                  `json:"first_record_cap"`
		Records        []snellV6FirstRecord `json:"records"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) < 32 {
		t.Fatalf("expected at least 32 vectors, got %d", len(vectors))
	}
	generators := map[uint32]bool{}
	for _, vector := range vectors {
		p := newSnellV6Profile([]byte(vector.PSK))
		if p.generator != vector.Generator || p.padMax != vector.PadMax || p.firstPayloadCap() != vector.FirstRecordCap {
			t.Fatalf("%s: profile generator=%d pad_max=%d cap=%d, kernel %d/%d/%d", vector.PSK, p.generator, p.padMax, p.firstPayloadCap(), vector.Generator, vector.PadMax, vector.FirstRecordCap)
		}
		generators[p.generator] = true
		for _, want := range vector.Records {
			if got := p.firstRecord(want.PayloadLen); got != want {
				t.Fatalf("%s payload %d: got %+v, kernel %+v", vector.PSK, want.PayloadLen, got, want)
			}
		}
	}
	if len(generators) != 4 {
		t.Fatalf("vectors cover generators %v, want all four", generators)
	}
}
