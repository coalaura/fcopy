//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

const chunkSize = 1 << 30

func nativeCopyFile(source, destination string, reflink reflinkMode) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}

	defer src.Close()

	dst, err := os.OpenFile(destination, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}

	defer dst.Close()

	if reflink != reflinkNever {
		err = unix.IoctlFileClone(int(dst.Fd()), int(src.Fd()))
		if err == nil {
			return nil
		}

		if reflink == reflinkAlways {
			return fmt.Errorf("create reflink: %w", err)
		}

		if !reflinkUnsupported(err) {
			return fmt.Errorf("create reflink: %w", err)
		}

		err = dst.Truncate(0)
		if err != nil {
			return err
		}
	}

	if reflink == reflinkNever {
		_, err = io.Copy(dst, src)
		return err
	}

	for {
		copied, err := unix.CopyFileRange(int(src.Fd()), nil, int(dst.Fd()), nil, chunkSize, 0)
		if err == nil {
			if copied == 0 {
				return nil
			}

			continue
		}

		if !copyFileRangeUnsupported(err) {
			return err
		}

		_, err = src.Seek(0, io.SeekStart)
		if err != nil {
			return err
		}

		_, err = dst.Seek(0, io.SeekStart)
		if err != nil {
			return err
		}

		err = dst.Truncate(0)
		if err != nil {
			return err
		}

		_, err = io.Copy(dst, src)
		return err
	}
}

func reflinkUnsupported(err error) bool {
	return errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTTY) || errors.Is(err, syscall.EOPNOTSUPP)
}

func copyFileRangeUnsupported(err error) bool {
	return errors.Is(err, syscall.ENOSYS) || errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.EOPNOTSUPP)
}

func preserveCopiedFileMetadata(path string, info fs.FileInfo) error {
	err := os.Chmod(path, info.Mode().Perm())
	if err != nil {
		return err
	}

	return os.Chtimes(path, info.ModTime(), info.ModTime())
}

func preserveDirectoryMetadata(path string, info fs.FileInfo) error {
	err := os.Chmod(path, info.Mode().Perm())
	if err != nil {
		return err
	}

	return os.Chtimes(path, info.ModTime(), info.ModTime())
}

func renameReplace(source, destination string) error {
	return os.Rename(source, destination)
}

func createSymbolicLink(target string, link string, targetIsDirectory bool) error {
	return os.Symlink(target, link)
}
