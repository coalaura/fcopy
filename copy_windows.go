//go:build windows

package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const symbolicLinkFlagAllowUnprivilegedCreate uint32 = 0x2

var copyFileW = windows.NewLazySystemDLL("kernel32.dll").NewProc("CopyFileW")

func nativeCopyFile(source, destination string, reflink reflinkMode) error {
	switch reflink {
	case reflinkAlways:
		return errors.New("reflink=always is not supported on Windows")
	case reflinkNever:
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

		_, err = io.Copy(dst, src)
		return err
	}

	sourceName, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}

	destinationName, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}

	result, _, callErr := copyFileW.Call(
		uintptr(unsafe.Pointer(sourceName)),
		uintptr(unsafe.Pointer(destinationName)),
		0, // bFailIfExists = FALSE
	)

	if result == 0 {
		return callErr
	}

	return nil
}

func preserveCopiedFileMetadata(_ string, _ fs.FileInfo) error {
	return nil
}

func preserveDirectoryMetadata(path string, info fs.FileInfo) error {
	err := os.Chmod(path, info.Mode().Perm())
	if err != nil {
		return err
	}

	return os.Chtimes(path, info.ModTime(), info.ModTime())
}

func renameReplace(source, destination string) error {
	sourceName, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}

	destinationName, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}

	return windows.MoveFileEx(sourceName, destinationName, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func createSymbolicLink(target string, link string, targetIsDirectory bool) error {
	targetName, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}

	linkName, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return err
	}

	flags := symbolicLinkFlagAllowUnprivilegedCreate
	if targetIsDirectory {
		flags |= windows.SYMBOLIC_LINK_FLAG_DIRECTORY
	}

	return windows.CreateSymbolicLink(linkName, targetName, flags)
}
