//go:build !windows && !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package compiler

import "fmt"

func lockBuildCache(path string) (func(), error) {
	return nil, fmt.Errorf("persistent build cache locking is unavailable")
}
