package servers

import (
	"encoding/json"
	"github.com/TypeTerrors/go_sync/internal/telemetry"
	"os"
	"path/filepath"
	"sync/atomic"
)

// Metrics count logical regular-file I/O, not kernel/physical disk traffic.
// They exclude database, metadata and protocol framing/TLS overhead. Counters
// include failed attempts and reset when the process restarts.
type Metrics struct {
	ReflinkBytes    atomic.Int64
	ScanRead        atomic.Int64
	SourceRead      atomic.Int64
	BasisRead       atomic.Int64
	StageWrite      atomic.Int64
	VerifyRead      atomic.Int64
	BackupRead      atomic.Int64
	BackupWrite     atomic.Int64
	PrefixRead      atomic.Int64
	PayloadSent     atomic.Int64
	PayloadReceived atomic.Int64
	Reused          atomic.Int64
	Transfers       atomic.Int64
	Failures        atomic.Int64
	TransferNanos   atomic.Int64
}
type MetricsSnapshot = telemetry.Metrics

func (e *Engine) Metrics() MetricsSnapshot {
	m := &e.metrics
	return MetricsSnapshot{ReflinkBytes: m.ReflinkBytes.Load(), ScanRead: m.ScanRead.Load(), SourceRead: m.SourceRead.Load(), BasisRead: m.BasisRead.Load(), StageWrite: m.StageWrite.Load(), VerifyRead: m.VerifyRead.Load(), BackupRead: m.BackupRead.Load(), BackupWrite: m.BackupWrite.Load(), PrefixRead: m.PrefixRead.Load(), PayloadSent: m.PayloadSent.Load(), PayloadReceived: m.PayloadReceived.Load(), Reused: m.Reused.Load(), Transfers: m.Transfers.Load(), Failures: m.Failures.Load(), TransferNanos: m.TransferNanos.Load()}
}

// WriteMetrics exports a private, atomically replaced JSON snapshot in state.
func (e *Engine) WriteMetrics() error {
	data, err := json.MarshalIndent(e.Metrics(), "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(e.stateDir, "metrics-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(e.stateDir, "metrics.json"))
}
