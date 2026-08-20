//go:build !windows

package config

import "os"

func replaceFileAtomic(source, destination string) error {
	return os.Rename(source, destination)
}
