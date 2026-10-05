package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile never truncates the previous configuration on a failed write.
func WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mitray-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return fmt.Errorf("写入配置: %w", err)
	}
	if closeErr != nil {
		return closeErr
	}
	return replace(f.Name(), path)
}
