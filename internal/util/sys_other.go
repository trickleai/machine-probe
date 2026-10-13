//go:build !linux

package util

import "os"

const ODirect = 0

func Fdatasync(f *os.File) error { return f.Sync() }

type FSStat struct{ Total, Free, Inodes, InodesFree uint64 }

func Statfs(string) (FSStat, bool) { return FSStat{}, false }

func DevOf(string) (uint64, bool) { return 0, false }
