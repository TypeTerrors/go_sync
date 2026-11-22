//go:build desktop && (linux || darwin)

package ui

import (
	"testing"
	"time"

	"github.com/TypeTerrors/go_sync/internal/configfile"
	"github.com/TypeTerrors/go_sync/internal/desktop/status"
	"github.com/TypeTerrors/go_sync/internal/telemetry"
)

func TestWindowRowsDistinguishLiveHistoricalAndPayload(t *testing.T) {
	now := time.Now()
	cfg := configfile.Description{SyncInterval: time.Second}
	m := status.Model{Fresh: true, Snapshot: telemetry.StatusSnapshot{
		Updated: now,
		Peers: []telemetry.PeerStatus{
			{Address: "b:1", LastSuccess: now, Pending: true},
			{Address: "a:1", LastSuccess: now, LastError: "certificate expired"},
			{Address: "c:1", LastSuccess: now.Add(-time.Second)},
			{Address: "d:1", LastSuccess: now.Add(-time.Hour)},
		},
		Transfers:   []telemetry.TransferProgress{{Path: "a<b>.bin", ReceivedBytes: 1024, TargetBytes: 8192, Started: now}},
		Unsupported: map[string]string{"link": "symlink"},
		Conflicts:   1,
	}}
	data := windowData("/profile", cfg, m, status.Service{State: status.Running}, now)
	peers := data["peerRows"].([]map[string]string)
	for i, want := range []string{"Needs attention", "Reconciling", "Recently reachable", "Waiting"} {
		if peers[i]["state"] != want {
			t.Fatalf("peer %d: %v", i, peers[i])
		}
	}
	transfer := data["transferRows"].([]map[string]string)[0]
	if transfer["name"] != "a<b>.bin" || transfer["received"] != "1.0 KiB" || transfer["target"] != "8.0 KiB" {
		t.Fatal(transfer)
	}
	if data["canChangeFolder"] != false || len(data["issueRows"].([]map[string]string)) != 3 {
		t.Fatal(data)
	}
	m.Fresh = false
	data = windowData("/profile", cfg, m, status.Service{State: status.Stopped}, now)
	for _, p := range data["peerRows"].([]map[string]string) {
		if p["state"] != "Historical" {
			t.Fatal(p)
		}
	}
	if data["canChangeFolder"] != true {
		t.Fatal("stopped folder cannot be changed")
	}
	data = windowData("/profile", cfg, m, status.Service{State: status.Unknown}, now)
	if data["canChangeFolder"] != false {
		t.Fatal("unknown service state must not permit switching")
	}
}

func TestFolderSwitchRejectsOverlappingRoots(t *testing.T) {
	for _, pair := range [][2]string{{"/data/a", "/data/a"}, {"/data/a", "/data/a/sub"}, {"/data/a/sub", "/data/a"}} {
		if !foldersOverlap(pair[0], pair[1]) {
			t.Fatal(pair)
		}
	}
	if foldersOverlap("/data/a", "/data/another") {
		t.Fatal("distinct folders overlap")
	}
}

func TestWindowEmptyStateHasNoSyntheticPeersOrTimestamp(t *testing.T) {
	data := windowData("", configfile.Description{}, status.Model{}, status.Service{State: status.Stopped}, time.Now())
	if data["heartbeat"] != "Not yet" || len(data["peerRows"].([]map[string]string)) != 0 || len(data["transferRows"].([]map[string]string)) != 0 {
		t.Fatal(data)
	}
}

func TestWindowIncludesConfigurationFailuresInAttention(t *testing.T) {
	m := status.Model{Health: status.Error, Title: "Configuration error", Detail: "certificate file is missing"}
	data := windowData("/profile", configfile.Description{}, m, status.Service{State: status.Stopped}, time.Now())
	issues := data["issueRows"].([]map[string]string)
	if len(issues) != 1 || issues[0]["detail"] != m.Detail {
		t.Fatal("configuration failure omitted from Attention", issues)
	}
}
