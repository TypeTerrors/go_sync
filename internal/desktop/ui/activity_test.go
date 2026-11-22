//go:build desktop && (linux || darwin)

package ui

import (
	"math"
	"testing"
	"time"

	"github.com/TypeTerrors/go_sync/internal/desktop/status"
	"github.com/TypeTerrors/go_sync/internal/telemetry"
)

func activityModel(at time.Time) status.Model {
	return status.Model{Fresh: true, Rates: status.Rates{Receive: 2000, Send: 3000, Valid: true},
		Snapshot: telemetry.StatusSnapshot{Updated: at, NodeID: "node", Generation: "session:1", PID: 10}}
}

func TestActivityUsesSnapshotTimeAndExpires(t *testing.T) {
	now := time.Unix(1000, 0)
	var history activityHistory
	m := activityModel(now)
	history.observe("config", m, now)
	history.observe("config", m, now.Add(time.Second))
	if len(history.points) != 1 {
		t.Fatal("duplicate snapshot invented another measurement")
	}
	m.Fresh = false
	if got := history.observe("config", m, now.Add(61*time.Second)); len(got) != 0 {
		t.Fatal("stale samples should expire without adding zero traffic")
	}
}

func TestActivityResetsIdentityAndRetainsScanGenerations(t *testing.T) {
	now := time.Unix(1000, 0)
	var history activityHistory
	m := activityModel(now)
	history.observe("config", m, now)
	m.Snapshot.Updated = now.Add(time.Second)
	m.Snapshot.Generation = "session:2"
	if got := history.observe("config", m, m.Snapshot.Updated); len(got) != 2 {
		t.Fatal("normal scan discarded history")
	}
	m.Snapshot.PID++
	if got := history.observe("config", m, m.Snapshot.Updated); len(got) != 1 {
		t.Fatal("process change retained old history")
	}
	m.Snapshot.Updated = now.Add(2 * time.Second)
	if got := history.observe("other-config", m, m.Snapshot.Updated); len(got) != 1 {
		t.Fatal("configuration change retained old history")
	}
	m.Snapshot.Generation = "new-session:1"
	if got := history.observe("other-config", m, m.Snapshot.Updated); len(got) != 1 {
		t.Fatal("session change retained old history")
	}
}

func TestActivityPreservesMissingMeasurementsAsGaps(t *testing.T) {
	now := time.Unix(1000, 0)
	for _, invalid := range []status.Rates{{}, {Receive: math.NaN(), Valid: true}, {Send: math.Inf(1), Valid: true}, {Receive: -1, Valid: true}} {
		var history activityHistory
		m := activityModel(now)
		history.observe("config", m, now)
		m.Snapshot.Updated = now.Add(time.Second)
		m.Rates = invalid
		points := history.observe("config", m, m.Snapshot.Updated)
		if len(points) != 2 || points[1].Valid || points[1].Receive != 0 || points[1].Send != 0 {
			t.Fatalf("invalid rates must serialize as an explicit gap: %+v", points)
		}
	}
}
