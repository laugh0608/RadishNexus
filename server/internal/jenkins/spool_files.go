package jenkins

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Parents are resolved once; a worker's directories must remain private to its
// trusted OS account. Neither agents nor repository code may write these paths.
func spoolDirectory(path string) error {
	if err := spoolFilesystem(path); err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil || real != path {
		return ErrSpool
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !spoolDirOwned(info) {
		return ErrSpool
	}
	return nil
}

func spoolRead(path string, limit int64) ([]byte, error) {
	f, err := spoolOpen(path, os.O_RDONLY)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, ErrSpool
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !spoolOwned(info) || info.Size() > limit {
		return nil, ErrSpoolState
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, ErrSpool
	}
	return b, nil
}

// atomicSpoolWrite is used for both initial publication and replacement, always
// under the private state-directory lock. A failed sync never reports success.
func atomicSpoolWrite(path string, body []byte, fault func(string) error) error {
	if len(body) > MaxSpoolRecord {
		return ErrSpoolCapacity
	}
	dir := filepath.Dir(path)
	check := func(stage string) error {
		if fault != nil {
			return fault(stage)
		}
		return nil
	}
	f, err := os.CreateTemp(dir, ".pending-")
	if err != nil {
		return ErrSpool
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	defer f.Close()
	if err = check("write"); err != nil {
		return ErrSpool
	}
	if _, err = f.Write(body); err != nil {
		return ErrSpool
	}
	if err = check("file_sync"); err != nil {
		return ErrSpool
	}
	if err = f.Sync(); err != nil {
		return ErrSpool
	}
	if err = f.Close(); err != nil {
		return ErrSpool
	}
	if err = check("rename"); err != nil {
		return ErrSpool
	}
	if err = os.Rename(temporary, path); err != nil {
		return ErrSpool
	}
	if err = check("directory_sync"); err != nil {
		return ErrSpool
	}
	d, err := os.Open(dir)
	if err != nil {
		return ErrSpool
	}
	defer d.Close()
	if err = d.Sync(); err != nil {
		return ErrSpool
	}
	return nil
}

// boundedNames avoids an unbounded ReadDir allocation for untrusted accumulation.
func boundedNames(dir string) ([]string, int64, error) {
	d, err := os.Open(dir)
	if err != nil {
		return nil, 0, ErrSpool
	}
	defer d.Close()
	var names []string
	var bytes int64
	for {
		entries, e := d.ReadDir(100)
		if e != nil && !errors.Is(e, io.EOF) {
			return nil, 0, ErrSpool
		}
		for _, entry := range entries {
			info, e := entry.Info()
			if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !spoolOwned(info) {
				return nil, 0, ErrSpoolState
			}
			names = append(names, entry.Name())
			bytes += info.Size()
			if len(names) > MaxSpoolEntries+4 || bytes > MaxSpoolBytes {
				return nil, 0, ErrSpoolCapacity
			}
		}
		if errors.Is(e, io.EOF) {
			break
		}
	}
	return names, bytes, nil
}
