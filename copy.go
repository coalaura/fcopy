package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

type copyOptions struct {
	workers int
	exclude *exclusionMatcher
}

type copyStats struct {
	files       int64
	directories int64
	symlinks    int64
	bytes       int64
}

type atomicCopyStats struct {
	files       atomic.Int64
	directories atomic.Int64
	symlinks    atomic.Int64
	bytes       atomic.Int64
}

func (stats *atomicCopyStats) load() copyStats {
	return copyStats{
		files:       stats.files.Load(),
		directories: stats.directories.Load(),
		symlinks:    stats.symlinks.Load(),
		bytes:       stats.bytes.Load(),
	}
}

type copyJob struct {
	source      string
	destination string
	info        fs.FileInfo
}

type directoryJob struct {
	path string
	info fs.FileInfo
}

func copyPath(ctx context.Context, source string, destination string, options copyOptions) (copyStats, error) {
	source, err := filepath.Abs(source)
	if err != nil {
		return copyStats{}, fmt.Errorf("resolve source path: %w", err)
	}

	destination, err = filepath.Abs(destination)
	if err != nil {
		return copyStats{}, fmt.Errorf("resolve destination path: %w", err)
	}

	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return copyStats{}, fmt.Errorf("inspect source %q: %w", source, err)
	}

	if !sourceInfo.IsDir() {
		destinationInfo, statErr := os.Stat(destination)

		switch {
		case statErr == nil && destinationInfo.IsDir():
			destination = filepath.Join(destination, filepath.Base(source))
		case statErr != nil && !errors.Is(statErr, fs.ErrNotExist):
			return copyStats{}, fmt.Errorf("inspect destination %q: %w", destination, statErr)
		}
	}

	err = validateCopyPaths(source, destination, sourceInfo.IsDir())
	if err != nil {
		return copyStats{}, err
	}

	if sourceInfo.IsDir() {
		return copyDirectory(ctx, source, destination, sourceInfo, options)
	}

	err = os.MkdirAll(filepath.Dir(destination), 0o755)
	if err != nil {
		return copyStats{}, fmt.Errorf("create destination parent %q: %w", filepath.Dir(destination), err)
	}

	var stats atomicCopyStats

	err = copyEntry(copyJob{
		source:      source,
		destination: destination,
		info:        sourceInfo,
	}, &stats)

	if err != nil {
		return copyStats{}, err
	}

	return stats.load(), nil
}

func copyDirectory(parentCtx context.Context, source string, destination string, sourceInfo fs.FileInfo, options copyOptions) (copyStats, error) {
	err := os.MkdirAll(filepath.Dir(destination), 0o755)
	if err != nil {
		return copyStats{}, fmt.Errorf("create destination parent %q: %w", filepath.Dir(destination), err)
	}

	err = prepareDirectory(destination, sourceInfo.Mode().Perm())
	if err != nil {
		return copyStats{}, fmt.Errorf("prepare destination directory %q: %w", destination, err)
	}

	ctx, cancel := context.WithCancelCause(parentCtx)
	defer cancel(nil)

	jobs := make(chan copyJob, options.workers*2)

	var (
		stats   atomicCopyStats
		workers sync.WaitGroup
	)

	for range options.workers {
		workers.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}

					err := copyEntry(job, &stats)
					if err != nil {
						cancel(fmt.Errorf("copy %q to %q: %w", job.source, job.destination, err))

						return
					}
				}
			}
		})
	}

	directories := make([]directoryJob, 0, 128)

	walkErr := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relativePath, err := filepath.Rel(source, path)
		if err != nil {
			return fmt.Errorf("resolve relative path for %q: %w", path, err)
		}

		if relativePath != "." && options.exclude.matches(relativePath) {
			if entry.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect %q: %w", path, err)
		}

		destinationPath := destination

		if relativePath != "." {
			destinationPath = filepath.Join(destination, relativePath)
		}

		if info.IsDir() {
			if relativePath != "." {
				err := prepareDirectory(destinationPath, info.Mode().Perm())
				if err != nil {
					return fmt.Errorf("prepare directory %q: %w", destinationPath, err)
				}
			}

			directories = append(directories, directoryJob{
				path: destinationPath,
				info: info,
			})

			stats.directories.Add(1)

			return nil
		}

		job := copyJob{
			source:      path,
			destination: destinationPath,
			info:        info,
		}

		select {
		case jobs <- job:
			return nil
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	},
	)

	if walkErr != nil {
		cancel(fmt.Errorf("walk source %q: %w", source, walkErr))
	}

	close(jobs)

	workers.Wait()

	err = context.Cause(ctx)
	if err != nil {
		return stats.load(), err
	}

	for i := len(directories) - 1; i >= 0; i-- {
		directory := directories[i]

		err = preserveDirectoryMetadata(directory.path, directory.info)
		if err != nil {
			return stats.load(), fmt.Errorf("preserve directory metadata for %q: %w", directory.path, err)
		}
	}

	return stats.load(), nil
}

func copyEntry(job copyJob, stats *atomicCopyStats) error {
	switch {
	case job.info.Mode().IsRegular():
		err := copyRegularFile(job)
		if err != nil {
			return err
		}

		stats.files.Add(1)
		stats.bytes.Add(job.info.Size())

		return nil
	case job.info.Mode()&fs.ModeSymlink != 0:
		err := copySymbolicLink(job)
		if err != nil {
			return err
		}

		stats.symlinks.Add(1)

		return nil
	default:
		return fmt.Errorf("unsupported file type %s", job.info.Mode().Type())
	}
}

func copyRegularFile(job copyJob) error {
	err := checkReplaceable(job.destination)
	if err != nil {
		return err
	}

	destinationDirectory := filepath.Dir(job.destination)

	temporary, err := os.CreateTemp(destinationDirectory, ".fastcopy-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}

	temporaryPath := temporary.Name()

	err = temporary.Close()
	if err != nil {
		os.Remove(temporaryPath)

		return fmt.Errorf("close temporary file: %w", err)
	}

	defer os.Remove(temporaryPath)

	err = nativeCopyFile(job.source, temporaryPath)
	if err != nil {
		return fmt.Errorf("copy file data: %w", err)
	}

	err = preserveCopiedFileMetadata(temporaryPath, job.info)
	if err != nil {
		return fmt.Errorf("preserve file metadata: %w", err)
	}

	err = renameReplace(temporaryPath, job.destination)
	if err != nil {
		return fmt.Errorf("replace destination: %w", err)
	}

	return nil
}

func copySymbolicLink(job copyJob) error {
	err := checkReplaceable(job.destination)
	if err != nil {
		return err
	}

	target, err := os.Readlink(job.source)
	if err != nil {
		return fmt.Errorf("read symbolic link: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(job.destination), ".fastcopy-link-*")
	if err != nil {
		return fmt.Errorf("reserve temporary symbolic link: %w", err)
	}

	temporaryPath := temporary.Name()

	err = temporary.Close()
	if err != nil {
		os.Remove(temporaryPath)

		return fmt.Errorf("close temporary placeholder: %w", err)
	}

	err = os.Remove(temporaryPath)
	if err != nil {
		return fmt.Errorf("remove temporary placeholder: %w", err)
	}

	defer os.Remove(temporaryPath)

	var targetIsDirectory bool

	targetInfo, err := os.Stat(job.source)
	if err == nil {
		targetIsDirectory = targetInfo.IsDir()
	}

	err = createSymbolicLink(target, temporaryPath, targetIsDirectory)
	if err != nil {
		return fmt.Errorf("create symbolic link: %w", err)
	}

	err = renameReplace(temporaryPath, job.destination)
	if err != nil {
		return fmt.Errorf("replace destination symbolic link: %w", err)
	}

	return nil
}

func prepareDirectory(path string, mode fs.FileMode) error {
	info, err := os.Lstat(path)

	switch {
	case err == nil:
		if info.Mode()&fs.ModeSymlink != 0 {
			return errors.New("destination is a symbolic link")
		}

		if !info.IsDir() {
			return errors.New("destination exists and is not a directory")
		}

		return nil
	case errors.Is(err, fs.ErrNotExist):
		err := os.Mkdir(path, mode)
		if err != nil {
			return err
		}

		return nil
	default:
		return err
	}
}

func checkReplaceable(path string) error {
	info, err := os.Lstat(path)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("inspect destination: %w", err)
	case info.Mode()&fs.ModeSymlink != 0:
		return errors.New("refusing to replace a destination symbolic link")
	case info.IsDir():
		return errors.New("destination is a directory")
	case !info.Mode().IsRegular():
		return fmt.Errorf("destination has unsupported type %s", info.Mode().Type())
	}

	return nil
}

func validateCopyPaths(source string, destination string, sourceIsDirectory bool) error {
	canonicalSource, err := canonicalPath(source)
	if err != nil {
		return fmt.Errorf("resolve source path %q: %w", source, err)
	}

	canonicalDestination, err := canonicalPath(destination)
	if err != nil {
		return fmt.Errorf("resolve destination path %q: %w", destination, err)
	}

	relativePath, err := filepath.Rel(canonicalSource, canonicalDestination)
	if err != nil {
		return nil
	}

	if relativePath == "." {
		return errors.New("source and destination are the same path")
	}

	if sourceIsDirectory && relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return errors.New("destination must not be inside the source directory")
	}

	return nil
}

func canonicalPath(path string) (string, error) {
	current := filepath.Clean(path)

	var missing []string

	for {
		_, err := os.Lstat(current)
		switch {
		case err == nil:
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}

			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}

			return filepath.Clean(resolved), nil
		case !errors.Is(err, fs.ErrNotExist):
			return "", err
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no existing path component for %q", path)
		}

		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
