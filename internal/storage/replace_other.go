//go:build !windows

package storage

import "os"

func replace(from, to string) error { return os.Rename(from, to) }
