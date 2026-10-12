//go:build windows

package layer

import "os"

func readlinkAbs(path string) (string, error) { return os.Readlink(path) }
