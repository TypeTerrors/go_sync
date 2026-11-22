package status

import (
	"encoding/json"
	"github.com/TypeTerrors/go_sync/internal/telemetry"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sample(now time.Time) telemetry.StatusSnapshot {
	return telemetry.StatusSnapshot{PID: 42, Updated: now, Running: true, Root: "/shared", NodeID: "node", Generation: "session:1", Files: 12, LastScan: now, Peers: []telemetry.PeerStatus{{Address: "peer:50051", LastSuccess: now, LastAttempt: now}}}
}
func TestReadMissingCorruptAndAtomicSnapshots(t *testing.T) {
	dir := t.TempDir()
	if _, err := Read(dir); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, data := range []string{"{", "null", "{}", `{"updated":"bad"}`} {
		if err := os.WriteFile(filepath.Join(dir, "status.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(dir); err == nil {
			t.Fatalf("accepted %q", data)
		}
	}
	want := sample(time.Now().UTC())
	data, _ := json.Marshal(want)
	if err := os.WriteFile(filepath.Join(dir, "next"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "next"), filepath.Join(dir, "status.json")); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir)
	if err != nil || got.PID != 42 || got.Files != 12 {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestHeartbeat(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		age           time.Duration
		running, want bool
	}{{0, true, true}, {5 * time.Second, true, true}, {6 * time.Second, true, false}, {0, false, false}, {-2 * time.Second, true, false}} {
		s := sample(now.Add(-tc.age))
		s.Running = tc.running
		if Fresh(s, now) != tc.want {
			t.Fatalf("%+v", tc)
		}
	}
}
func TestMeterUsesGenerationSessionNotEveryFileGeneration(t *testing.T) {
	now := time.Now()
	s := sample(now)
	s.Metrics.PayloadSent = 100
	s.Metrics.PayloadReceived = 300
	var m Meter
	if m.Observe(s, now).Valid {
		t.Fatal("first sample has no rate")
	}
	s.Updated = now.Add(time.Second)
	s.Generation = "session:2"
	s.Metrics.PayloadSent = 600
	s.Metrics.PayloadReceived = 1300
	r := m.Observe(s, s.Updated)
	if !r.Valid || r.Send != 500 || r.Receive != 1000 {
		t.Fatal(r)
	}
	if again := m.Observe(s, s.Updated.Add(100*time.Millisecond)); again != r {
		t.Fatal("duplicate timestamp destroyed rate", again)
	}
	s.Updated = s.Updated.Add(time.Second)
	s.Generation = "restarted:1"
	s.Metrics.PayloadSent += 10000
	if m.Observe(s, s.Updated).Valid {
		t.Fatal("restart must invalidate even if counters exceed previous")
	}
	s.Updated = s.Updated.Add(time.Second)
	s.Metrics.PayloadSent = 0
	if m.Observe(s, s.Updated).Valid {
		t.Fatal("counter reset underflow")
	}
	if m.Observe(s, s.Updated.Add(6*time.Second)).Valid {
		t.Fatal("stale status retained rate")
	}
}
func TestClassify(t *testing.T) {
	now := time.Now()
	base := sample(now)
	cases := []struct {
		name   string
		state  Service
		edit   func(*telemetry.StatusSnapshot)
		err    error
		health Health
	}{
		{"idle", Service{State: Running, PID: 42}, nil, nil, Idle},
		{"transferring", Service{State: Running, PID: 42}, func(s *telemetry.StatusSnapshot) { s.ActiveDownloads = 2 }, nil, Syncing},
		{"stopped", Service{State: Stopped}, func(s *telemetry.StatusSnapshot) { s.Running = false }, nil, Health(Stopped)},
		{"starting", Service{State: Starting}, nil, nil, Health(Starting)},
		{"failed", Service{State: Failed}, nil, nil, Error},
		{"scan error", Service{State: Running, PID: 42}, func(s *telemetry.StatusSnapshot) { s.ScanError = "disk full" }, nil, Error},
		{"conflict", Service{State: Running, PID: 42}, func(s *telemetry.StatusSnapshot) { s.Conflicts = 1 }, nil, Warning},
		{"missing peer", Service{State: Running, PID: 42}, func(s *telemetry.StatusSnapshot) { s.Peers = nil }, nil, Warning},
		{"offline peer", Service{State: Running, PID: 42}, func(s *telemetry.StatusSnapshot) {
			s.Peers = []telemetry.PeerStatus{{LastSuccess: now.Add(-time.Minute)}}
		}, nil, Warning},
		{"external", Service{State: Stopped}, nil, nil, Warning},
		{"different pid", Service{State: Running, PID: 99}, nil, nil, Warning},
		{"stale", Service{State: Running, PID: 42}, func(s *telemetry.StatusSnapshot) { s.Updated = now.Add(-time.Minute) }, nil, Unavailable},
		{"missing snapshot", Service{State: Running, PID: 42}, nil, os.ErrNotExist, Unavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			if tc.edit != nil {
				tc.edit(&s)
			}
			m := Classify(tc.state, s, tc.err, now, time.Second, Rates{1, 2, true})
			if m.Health != tc.health {
				t.Fatalf("%+v", m)
			}
			if !m.Fresh && (m.Rates.Valid || m.Transfers != 0 || m.Peers != 0) {
				t.Fatal("stale activity displayed")
			}
		})
	}
	m := Classify(Service{State: Running, PID: 42}, base, nil, now, time.Second, Rates{})
	if m.Peers != 1 || !strings.Contains(m.Detail, "not proof") || strings.Contains(m.Title, "fully synced") {
		t.Fatal(m)
	}
}
func TestDisplayDoesNotCallPayloadACompletionPercentage(t *testing.T) {
	s := sample(time.Now())
	s.Transfers = []telemetry.TransferProgress{{Path: "video.mp4", ReceivedBytes: 10, TargetBytes: 100}}
	m := Classify(Service{State: Running, PID: 42}, s, nil, time.Now(), time.Second, Rates{})
	if !strings.Contains(m.Details(), "not a completion percentage") || !strings.Contains(m.Details(), "peer:50051") {
		t.Fatal(m.Details())
	}
}

func TestDeferredReconciliationIsNotConnectivityFailure(t *testing.T) {
	now := time.Now()
	s := sample(now)
	s.Peers[0].Pending = true
	m := Classify(Service{State: Running, PID: 42}, s, nil, now, time.Second, Rates{})
	if m.Health != Syncing || m.Title != "Reconciliation pending" {
		t.Fatal(m)
	}
}
