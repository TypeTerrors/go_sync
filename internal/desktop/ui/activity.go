//go:build desktop && (linux || darwin)

package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/TypeTerrors/go_sync/internal/desktop/status"
)

const activityWindow = time.Minute

type activityPoint struct {
	Time    int64   `json:"time"`
	Receive float64 `json:"receive"`
	Send    float64 `json:"send"`
	Valid   bool    `json:"valid"`
}

// History follows snapshot time, not UI polling time. Duplicate snapshots do
// not invent new measurements; gaps and process/config changes break the line.
type activityHistory struct {
	identity string
	updated  time.Time
	points   []activityPoint
}

func (h *activityHistory) observe(path string, m status.Model, now time.Time) []activityPoint {
	s := m.Snapshot
	identity := fmt.Sprintf("%s\x00%s\x00%s\x00%d", path, s.NodeID, strings.SplitN(s.Generation, ":", 2)[0], s.PID)
	if identity != h.identity || (!s.Updated.IsZero() && s.Updated.Before(h.updated)) {
		*h = activityHistory{identity: identity}
	}
	if m.Fresh && s.Updated.After(h.updated) {
		valid := m.Rates.Valid && finiteRate(m.Rates.Receive) && finiteRate(m.Rates.Send)
		point := activityPoint{Time: s.Updated.UnixMilli(), Valid: valid}
		if valid {
			point.Receive, point.Send = m.Rates.Receive, m.Rates.Send
		}
		h.points = append(h.points, point)
		h.updated = s.Updated
	}
	cutoff := now.Add(-activityWindow).UnixMilli()
	for len(h.points) > 0 && h.points[0].Time < cutoff {
		h.points = h.points[1:]
	}
	return h.points
}

func finiteRate(n float64) bool { return n >= 0 && !math.IsNaN(n) && !math.IsInf(n, 0) }
