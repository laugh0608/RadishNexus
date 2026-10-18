//go:build linux || darwin

package jenkins

import (
	"errors"
	"os"
	"runtime"
	"syscall"
)

func spoolFilesystem(path string) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return ErrSpool
	}
	if runtime.GOOS == "linux" {
		// Local filesystems implementing rename/fsync. tmpfs and ephemeral
		// overlay storage only provide process-restart recovery, not host loss.
		switch uint64(stat.Type) {
		case 0xef53, 0x58465342, 0x9123683e, 0x01021994, 0x794c7630:
		default:
			return ErrSpool
		}
	}
	return nil
}

func spoolLock(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return ErrSpoolLocked
		}
		return ErrSpool
	}
	return nil
}
func spoolOwned(info os.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid()) && s.Nlink == 1
}
func spoolDirOwned(info os.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid())
}
func spoolOpen(path string, flags int) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
