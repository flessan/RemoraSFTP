package transfers

import (
	"io"
	"os"
	"strings"
)

// moveFile renames src to dst, falling back to copy+unlink when src and dst
// live on different filesystems (os.Rename returns EXDEV - e.g. the app data
// tmp area on one filesystem and the download destination on another).
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDevice(err) {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	_ = os.Chmod(dst, info.Mode().Perm())
	return os.Remove(src)
}

func isCrossDevice(err error) bool {
	if err == nil {
		return false
	}
	if le, ok := err.(*os.LinkError); ok {
		err = le.Err
	}
	return strings.Contains(err.Error(), "cross-device")
}
