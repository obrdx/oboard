package capability

func serverMonitoringDisplaySchema() map[string]any {
	sample := closedObject(map[string]any{
		"available":     map[string]any{"type": "boolean"},
		"latency_ms":    map[string]any{"type": []string{"number", "null"}},
		"sample_count":  map[string]any{"type": "integer"},
		"success_count": map[string]any{"type": "integer"},
		"checked_at":    map[string]any{"type": "string"},
	}, "available", "latency_ms", "sample_count", "success_count", "checked_at")
	grade := closedObject(map[string]any{
		"excellent_ms": map[string]any{"type": "integer", "description": "Latency at or below this is excellent for this server region and target."},
		"fair_ms":      map[string]any{"type": "integer", "description": "Latency at or below this is acceptable; slower is poor."},
		"basis":        map[string]any{"type": "string", "enum": []string{"route", "fixed"}, "description": "route: derived from the server region and the target province; fixed: region-independent fallback."},
	}, "excellent_ms", "fair_ms", "basis")
	series := closedObject(map[string]any{
		"probe_id": map[string]any{"type": "string"},
		"label":    map[string]any{"type": "string"},
		"family":   map[string]any{"type": "string", "enum": []string{"ipv4", "ipv6"}},
		"grade":    grade,
		"samples":  map[string]any{"type": "array", "maxItems": 20, "items": sample},
	}, "probe_id", "label", "grade", "samples")
	return closedObject(map[string]any{
		"name":    map[string]any{"type": "string"},
		"enabled": map[string]any{"type": "boolean"},
		"series":  map[string]any{"type": "array", "maxItems": 4, "items": series},
	}, "name", "enabled", "series")
}
