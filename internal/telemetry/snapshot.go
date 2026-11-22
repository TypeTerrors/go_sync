// Package telemetry defines the daemon's local, read-only JSON snapshots.
package telemetry

import "time"

type TransferProgress struct {
	Path          string    `json:"path"`
	TargetBytes   int64     `json:"target_bytes"`
	ReceivedBytes int64     `json:"payload_received_this_attempt"`
	Started       time.Time `json:"started"`
}
type PeerStatus struct {
	Address     string    `json:"address"`
	NodeID      string    `json:"node_id,omitempty"`
	LastAttempt time.Time `json:"last_attempt"`
	LastSuccess time.Time `json:"last_success"`
	Pending     bool      `json:"pending"`
	LastError   string    `json:"last_error,omitempty"`
}
type StatusSnapshot struct {
	PID             int                `json:"pid,omitempty"`
	Updated         time.Time          `json:"updated"`
	Running         bool               `json:"running"`
	NodeID          string             `json:"node_id"`
	FolderID        string             `json:"folder_id"`
	Root            string             `json:"root"`
	Generation      string             `json:"generation"`
	Files           int                `json:"files"`
	Tombstones      int                `json:"tombstones"`
	DeletionFloors  int                `json:"deletion_floors"`
	Conflicts       int                `json:"conflicts"`
	Partials        int                `json:"partial_transfers"`
	ActiveUploads   int                `json:"active_uploads"`
	ActiveDownloads int                `json:"active_downloads"`
	LastScan        time.Time          `json:"last_scan"`
	ScanError       string             `json:"scan_error,omitempty"`
	Unsupported     map[string]string  `json:"unsupported_paths,omitempty"`
	Transfers       []TransferProgress `json:"transfers"`
	Peers           []PeerStatus       `json:"peers"`
	Metrics         Metrics            `json:"metrics"`
}
type Metrics struct {
	ReflinkBytes    int64 `json:"reflink_bytes"`
	ScanRead        int64 `json:"scan_read_bytes"`
	SourceRead      int64 `json:"source_read_bytes"`
	BasisRead       int64 `json:"basis_read_bytes"`
	StageWrite      int64 `json:"stage_write_bytes"`
	VerifyRead      int64 `json:"verify_read_bytes"`
	BackupRead      int64 `json:"backup_read_bytes"`
	BackupWrite     int64 `json:"backup_write_bytes"`
	PrefixRead      int64 `json:"prefix_read_bytes"`
	PayloadSent     int64 `json:"payload_sent_bytes"`
	PayloadReceived int64 `json:"payload_received_bytes"`
	Reused          int64 `json:"reused_bytes"`
	Transfers       int64 `json:"transfer_attempts"`
	Failures        int64 `json:"transfer_failures"`
	TransferNanos   int64 `json:"transfer_nanoseconds"`
}

func (s Metrics) ReadBytes() int64 {
	return s.ScanRead + s.SourceRead + s.BasisRead + s.VerifyRead + s.BackupRead + s.PrefixRead
}
func (s Metrics) WriteBytes() int64 { return s.StageWrite + s.BackupWrite }
