//go:build !windows

package host

import (
	"os"
	"syscall"
)

func copyConfigMetadata(dst *os.File, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if err := dst.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
			return err
		}
	}
	return dst.Chmod(info.Mode().Perm())
}
