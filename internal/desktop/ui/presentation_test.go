//go:build desktop && (linux || darwin)

package ui

import (
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TypeTerrors/go_sync/internal/configfile"
	"github.com/TypeTerrors/go_sync/internal/desktop/status"
)

func TestDashboardUsesRealCountersAndHonestIdle(t *testing.T) {
	now := time.Now()
	a := &App{}
	m := activityModel(now)
	m.Health, m.Title, m.Peers, m.KnownPeers, m.Files, m.Transfers = status.Idle, "Running", 2, 3, 1284, 0
	cfg := configfile.Description{Folder: filepath.Join(t.TempDir(), "Studio & <work>")}
	d := a.dashboardData("config", m, cfg, now)
	if d["peers"] != "2 / 3" || d["files"] != "1284" || d["receive"] != "2.0 kB/s" || d["security"] != "Mutual TLS" {
		t.Fatalf("wrong display: %#v", d)
	}
	if d["folder"] != "Studio & <work>" || !strings.Contains(d["summary"].(string), "catching up") {
		t.Fatalf("lost folder or convergence caveat: %#v", d)
	}
	m.Fresh = false
	d = a.dashboardData("config", m, cfg, now.Add(6*time.Second))
	for _, key := range []string{"peers", "files", "transfers", "receive", "send"} {
		if d[key] != "—" {
			t.Fatalf("stale %s presented as live: %v", key, d[key])
		}
	}
}

func TestDashboardSecurityAndInvalidMeasurements(t *testing.T) {
	a, now := &App{}, time.Now()
	m := activityModel(now)
	m.Rates.Receive = math.NaN()
	d := a.dashboardData("config", m, configfile.Description{}, now)
	if d["security"] != "Unverified" || d["receive"] != "—" {
		t.Fatalf("claimed unvalidated config or rate: %#v", d)
	}
	if _, err := json.Marshal(d); err != nil {
		t.Fatalf("invalid traffic poisoned renderer payload: %v", err)
	}
	d = a.dashboardData("", m, configfile.Description{}, now)
	if d["security"] != "No folder" || d["title"] != "Not configured" {
		t.Fatal(d)
	}
	d = a.dashboardData("config", m, configfile.Description{Folder: "/workspace", Insecure: true}, now)
	if d["security"] != "Open LAN" {
		t.Fatal(d)
	}
}
