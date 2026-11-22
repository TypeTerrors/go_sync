package configfile

import "time"

// Description contains resolved paths and public settings, never key contents.
// The daemon is the authority for parsing and validating its own configuration.
type Description struct {
	ConfigPath    string        `json:"config_path"`
	Folder        string        `json:"folder"`
	StateDir      string        `json:"state_dir"`
	FolderID      string        `json:"folder_id"`
	Insecure      bool          `json:"insecure"`
	ExplicitState bool          `json:"explicit_state"`
	Bootstrap     string        `json:"bootstrap"`
	SyncInterval  time.Duration `json:"sync_interval"`
}
