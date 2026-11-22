// Package status reads the daemon's atomic snapshots without opening its database.
package status

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TypeTerrors/go_sync/internal/telemetry"
)

const HeartbeatLimit = 5 * time.Second

type ServiceState string

const (
	Unknown  ServiceState = "unknown"
	Stopped  ServiceState = "stopped"
	Starting ServiceState = "starting"
	Running  ServiceState = "running"
	Failed   ServiceState = "failed"
)

type Service struct {
	State              ServiceState
	Installed, Enabled bool
	PID                int
	Detail             string
	ConfigPath         string
}

type Health string

const (
	Idle        Health = "idle"
	Syncing     Health = "syncing"
	Warning     Health = "warning"
	Error       Health = "error"
	Unavailable Health = "unavailable"
)

type Rates struct {
	Receive, Send float64
	Valid         bool
}
type Meter struct {
	previous telemetry.StatusSnapshot
	have     bool
	rates    Rates
}

// Observe uses counters embedded in status.json, which are the same counters as
// metrics.json but have a timestamp and generation in one atomic snapshot.
func (m *Meter) Observe(s telemetry.StatusSnapshot, now time.Time) Rates {
	if !Fresh(s, now) {
		m.Reset()
		return Rates{}
	}
	if m.have && s.Updated.Equal(m.previous.Updated) {
		return m.rates
	}
	session := func(g string) string { return strings.SplitN(g, ":", 2)[0] }
	dt := s.Updated.Sub(m.previous.Updated).Seconds()
	m.rates = Rates{}
	if m.have && dt > 0 && dt <= HeartbeatLimit.Seconds() && s.NodeID == m.previous.NodeID &&
		session(s.Generation) == session(m.previous.Generation) && s.PID == m.previous.PID &&
		s.Metrics.PayloadReceived >= m.previous.Metrics.PayloadReceived && s.Metrics.PayloadSent >= m.previous.Metrics.PayloadSent {
		m.rates = Rates{float64(s.Metrics.PayloadReceived-m.previous.Metrics.PayloadReceived) / dt,
			float64(s.Metrics.PayloadSent-m.previous.Metrics.PayloadSent) / dt, true}
	}
	m.previous, m.have = s, true
	return m.rates
}
func (m *Meter) Reset() { *m = Meter{} }
func Fresh(s telemetry.StatusSnapshot, now time.Time) bool {
	age := now.Sub(s.Updated)
	return s.Running && !s.Updated.IsZero() && age >= -time.Second && age <= HeartbeatLimit
}

func Read(state string) (telemetry.StatusSnapshot, error) {
	var s telemetry.StatusSnapshot
	if state == "" {
		return s, fmt.Errorf("no state directory selected")
	}
	f, err := os.Open(filepath.Join(state, "status.json"))
	if err != nil {
		return s, err
	}
	defer f.Close()
	const limit = 16 << 20
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return s, err
	}
	if len(data) > limit {
		return s, fmt.Errorf("status snapshot exceeds 16 MiB")
	}
	if err = json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("invalid status snapshot: %w", err)
	}
	if s.Updated.IsZero() || s.Root == "" || s.NodeID == "" || s.Generation == "" || s.Files < 0 ||
		s.ActiveDownloads < 0 || s.ActiveUploads < 0 || s.Metrics.PayloadSent < 0 || s.Metrics.PayloadReceived < 0 {
		return s, fmt.Errorf("status snapshot has missing or invalid fields")
	}
	return s, nil
}

func Matches(s telemetry.StatusSnapshot, folder, folderID string) bool {
	if folder == "" || s.FolderID != folderID {
		return false
	}
	if filepath.Clean(s.Root) == filepath.Clean(folder) {
		return true
	}
	resolved, err := filepath.EvalSymlinks(folder)
	return err == nil && filepath.Clean(resolved) == filepath.Clean(s.Root)
}

type Model struct {
	Health                              Health
	Title, Detail                       string
	Files, Peers, KnownPeers, Transfers int
	Rates                               Rates
	Snapshot                            telemetry.StatusSnapshot
	Fresh                               bool
	Managed                             bool
}

func Classify(service Service, s telemetry.StatusSnapshot, readErr error, now time.Time, interval time.Duration, rates Rates) Model {
	m := Model{Snapshot: s, Rates: rates, Files: s.Files, KnownPeers: len(s.Peers), Transfers: s.ActiveUploads + s.ActiveDownloads,
		Fresh: readErr == nil && Fresh(s, now), Managed: service.State == Running && (s.PID == 0 || service.PID == s.PID)}
	window := max(30*time.Second, 3*interval)
	peerIssue := false
	pending := false
	for _, p := range s.Peers {
		age := now.Sub(p.LastSuccess)
		if !p.LastSuccess.IsZero() && age >= -time.Second && age <= window {
			m.Peers++
		}
		pending = pending || p.Pending
		if p.LastError != "" || p.LastSuccess.IsZero() || age > window {
			peerIssue = true
		}
	}
	switch {
	case service.State == Unknown:
		m.Health, m.Title, m.Detail = Unavailable, "Service status unavailable", service.Detail
	case service.State == Failed:
		m.Health, m.Title, m.Detail = Error, "Service failed", service.Detail
	case service.State == Starting:
		m.Health, m.Title, m.Detail = Health(Starting), "Starting", "Waiting for the daemon to report a fresh heartbeat."
	case !m.Fresh && service.State == Stopped:
		m.Health, m.Title, m.Detail = Health(Stopped), "Stopped", "Synchronization is stopped."
	case !m.Fresh:
		m.Health, m.Title, m.Detail = Unavailable, "Status unavailable", "The daemon heartbeat is stale or unavailable."
		if readErr != nil {
			m.Detail = readErr.Error()
		}
	case !m.Managed:
		m.Health, m.Title, m.Detail = Warning, "Running externally", "A CLI or another service owns this state. The tray will not stop or replace it."
	case s.ScanError != "":
		m.Health, m.Title, m.Detail = Error, "Scan error", s.ScanError
	case s.Conflicts > 0 || len(s.Unsupported) > 0:
		m.Health, m.Title, m.Detail = Warning, "Needs attention", fmt.Sprintf("%d conflicts; %d unsupported paths. See details before changing files.", s.Conflicts, len(s.Unsupported))
	case peerIssue || len(s.Peers) == 0:
		m.Health, m.Title, m.Detail = Warning, "Waiting for peers", "Peer reachability is incomplete. Local idle does not prove that every peer has converged."
	case m.Transfers > 0:
		m.Health, m.Title, m.Detail = Syncing, "Synchronizing", "Transfers are active. Payload rates exclude protocol overhead and reused bytes."
	case pending:
		m.Health, m.Title, m.Detail = Syncing, "Reconciliation pending", "The engine is retrying or yielding work; no active payload transfer is visible in this snapshot."
	default:
		m.Health, m.Title, m.Detail = Idle, "Running · locally idle", "No active local transfers. This is not proof of convergence across every peer."
	}
	if !m.Fresh {
		m.Rates = Rates{}
		m.Peers = 0
		m.Transfers = 0
	}
	return m
}

func (m Model) Details() string {
	s := m.Snapshot
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n\nFolder: %s\nNode: %s\nGroup: %s\nLast heartbeat: %s\nLast scan: %s\nTracked entries: %d\nConflicts: %d\nPending partial files: %d\n\n", m.Title, m.Detail, s.Root, s.NodeID, s.FolderID, s.Updated.Format(time.RFC3339), s.LastScan.Format(time.RFC3339), s.Files, s.Conflicts, s.Partials)
	if !m.Fresh {
		b.WriteString("The following is historical, unverified snapshot data.\n")
	}
	for _, p := range s.Peers {
		fmt.Fprintf(&b, "Peer %s (%s)\n  Last successful reconciliation: %s\n  Pending: %t  Error: %s\n", p.Address, p.NodeID, p.LastSuccess.Format(time.RFC3339), p.Pending, p.LastError)
	}
	for _, t := range s.Transfers {
		fmt.Fprintf(&b, "Receiving %s: %d payload bytes this attempt; target %d bytes (not a completion percentage)\n", t.Path, t.ReceivedBytes, t.TargetBytes)
	}
	fmt.Fprintf(&b, "\nProcess payload totals: received %d bytes, sent %d bytes\nTransfer attempts: %d; failures: %d (cumulative, not current errors)\n", s.Metrics.PayloadReceived, s.Metrics.PayloadSent, s.Metrics.Transfers, s.Metrics.Failures)
	return b.String()
}
