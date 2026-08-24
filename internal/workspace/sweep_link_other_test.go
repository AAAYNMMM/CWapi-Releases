//go:build !windows

package workspace

import "os"

func createDirectoryLink(target, link string) error {
	return os.Symlink(target, link)
}
