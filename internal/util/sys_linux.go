//go:build linux

package util

import (
	"os"
	"syscall"
)

const ODirect = syscall.O_DIRECT

func Fdatasync(f *os.File) error { return syscall.Fdatasync(int(f.Fd())) }

type FSStat struct{ Total, Free, Inodes, InodesFree uint64 }

func Statfs(path string) (FSStat, bool) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return FSStat{}, false
	}
	bs := uint64(s.Bsize)
	return FSStat{Total: uint64(s.Blocks) * bs, Free: uint64(s.Bavail) * bs, Inodes: uint64(s.Files), InodesFree: uint64(s.Ffree)}, true
}

func DevOf(path string) (uint64, bool) {
	var s syscall.Stat_t
	if err := syscall.Stat(path, &s); err != nil {
		return 0, false
	}
	return uint64(s.Dev), true
}
