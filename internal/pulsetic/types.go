package pulsetic

import (
	"encoding/json"
	"fmt"
)

// MonitorRef and StatusPageRef are intentionally minimal: the audit tool
// only needs IDs to iterate per-monitor history endpoints. Full monitor
// and status-page payloads are preserved verbatim as raw response bytes
// in the audit log, so we don't maintain a schema here.
type MonitorRef struct {
	ID int64 `json:"id"`
}

type StatusPageRef struct {
	ID int64 `json:"id"`
}

// ExtractMonitorIDs parses a monitor list response body and returns the
// IDs found. It tolerates both {"data":[...]} and bare-array response
// shapes since Pulsetic's docs show only examples - the envelope is not
// contractually spec'd.
func ExtractMonitorIDs(body []byte) ([]int64, error) {
	items, err := ExtractArray(body)
	if err != nil {
		return nil, fmt.Errorf("extract monitors: %w", err)
	}
	ids := make([]int64, 0, len(items))
	for i, raw := range items {
		var m MonitorRef
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("monitor[%d]: %w", i, err)
		}
		if m.ID == 0 {
			continue
		}
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// ExtractStatusPageIDs mirrors ExtractMonitorIDs for status pages.
func ExtractStatusPageIDs(body []byte) ([]int64, error) {
	items, err := ExtractArray(body)
	if err != nil {
		return nil, fmt.Errorf("extract status pages: %w", err)
	}
	ids := make([]int64, 0, len(items))
	for i, raw := range items {
		var s StatusPageRef
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("status_page[%d]: %w", i, err)
		}
		if s.ID == 0 {
			continue
		}
		ids = append(ids, s.ID)
	}
	return ids, nil
}

// ExtractArray returns the top-level items from a response whether the
// envelope is {"data":[...]} or a bare array.
func ExtractArray(body []byte) ([]json.RawMessage, error) {
	if len(body) == 0 {
		return nil, nil
	}
	// Try object envelope first.
	var env struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Data != nil {
		return env.Data, nil
	}
	// Fall back to bare array.
	var arr []json.RawMessage
	if err := json.Unmarshal(body, &arr); err == nil {
		return arr, nil
	}
	return nil, fmt.Errorf("unrecognised response envelope")
}
