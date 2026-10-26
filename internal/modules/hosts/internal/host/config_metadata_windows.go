package host

import "os"

func copyConfigMetadata(dst *os.File, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return dst.Chmod(info.Mode().Perm())
}
