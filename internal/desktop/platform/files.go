package platform

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func WritePrivate(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to replace non-regular file %s", name)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(name), ".gosync-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), name)
}

const configMarker = "GoSync-Configuration: "

func markConfig(path string) string {
	return configMarker + base64.RawStdEncoding.EncodeToString([]byte(path))
}
func markedConfig(data []byte) string {
	_, tail, ok := strings.Cut(string(data), configMarker)
	if !ok {
		return ""
	}
	fields := strings.Fields(tail)
	if len(fields) == 0 {
		return ""
	}
	value, err := base64.RawStdEncoding.DecodeString(fields[0])
	if err != nil {
		return ""
	}
	return string(value)
}

func removeOwned(path, marker string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), marker) {
		return fmt.Errorf("refusing to remove an unmanaged file: %s", path)
	}
	return os.Remove(path)
}

func writeManaged(path string, data []byte, marker string) error {
	old, err := os.ReadFile(path)
	if err == nil && !strings.Contains(string(old), marker) {
		return fmt.Errorf("existing custom integration is preserved: %s", path)
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return WritePrivate(path, data)
}
