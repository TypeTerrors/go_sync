//go:build linux || darwin

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// StateBusy probes the directory flock used by Badger. It neither opens the
// database nor writes its advisory LOCK file. The daemon's lock remains the
// authoritative check if another process starts after this probe.
func StateBusy(path string) (bool, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	var st unix.Stat_t
	if err = unix.Fstat(int(f.Fd()), &st); err != nil {
		return false, err
	}
	if st.Uid != uint32(os.Geteuid()) {
		return false, fmt.Errorf("state directory is not owned by the current user")
	}
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, unix.Flock(int(f.Fd()), unix.LOCK_UN)
}

// LockTray is held for the UI lifetime; it never locks daemon state. The lock
// file is not removed (unlinking a flock file would allow two different inodes).
func LockTray(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("GoSync Desktop is already running: %w", err)
	}
	return f, nil
}
