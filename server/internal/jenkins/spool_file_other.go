//go:build !linux && !darwin

package jenkins

import "os"

func spoolFilesystem(string) error { return ErrSpool }

func spoolLock(*os.File) error                { return ErrSpool }
func spoolOwned(os.FileInfo) bool             { return false }
func spoolDirOwned(os.FileInfo) bool          { return false }
func spoolOpen(string, int) (*os.File, error) { return nil, ErrSpool }
