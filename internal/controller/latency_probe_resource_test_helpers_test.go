package controller

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/core"
	"github.com/OboardProject/oboard/internal/model"
)

// probeResourceBody renders a v2 probe resource. Each preset is
// "<id>|<province>|<carrier>|<targetID>=<host>;<targetID>=<host>".
func probeResourceBody(presets ...string) []byte {
	items := []model.LatencyProbePreset{}
	for _, spec := range presets {
		parts := strings.SplitN(spec, "|", 4)
		preset := model.LatencyProbePreset{ID: parts[0], Province: parts[1], Carrier: parts[2], Targets: []model.LatencyProbePresetTarget{}}
		if len(parts) == 4 && parts[3] != "" {
			for _, target := range strings.Split(parts[3], ";") {
				id, host, _ := strings.Cut(target, "=")
				preset.Targets = append(preset.Targets, model.LatencyProbePresetTarget{ID: id, Family: core.LatencyProbeTargetFamily(id), Host: host})
			}
		}
		items = append(items, preset)
	}
	body, _ := json.Marshal(map[string]any{"schema": latencyProbeResourceSchema, "presets": items})
	return body
}

func probeResource(t *testing.T, presets ...string) latencyProbeResource {
	t.Helper()
	resource, err := parseLatencyProbeResource(probeResourceBody(presets...), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return resource
}
