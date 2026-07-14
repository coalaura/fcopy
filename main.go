package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/coalaura/plain"
	"github.com/urfave/cli/v3"
)

var logger = plain.New(plain.WithTarget(os.Stderr))

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	defaultWorkers := min(64, max(4, runtime.GOMAXPROCS(0)*4))

	command := &cli.Command{
		Name:      "fastcopy",
		Usage:     "copy files and directory trees quickly",
		ArgsUsage: "SOURCE DESTINATION",
		Description: `Copies a file or directory tree.

For a directory source, DESTINATION is the copied tree's root. For a file
source, an existing destination directory receives SOURCE's base name.

Exclusion examples:
  --exclude '*.tmp'
  --exclude '.git'
  --exclude 'build/**'
  --exclude '**/generated/*.go'`,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "workers",
				Usage: "maximum number of files copied concurrently",
				Value: defaultWorkers,
			},
			&cli.StringSliceFlag{
				Name:  "exclude",
				Usage: "exclude a relative path or glob pattern; may be repeated",
			},
		},
		Action: run,
	}

	err := command.Run(ctx, os.Args)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Warnln("copy interrupted")
		} else {
			logger.Errorf("fastcopy: %v\n", err)
		}

		os.Exit(1)
	}
}

func run(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 2 {
		return errors.New("expected SOURCE and DESTINATION")
	}

	workers := command.Int("workers")
	if workers < 1 {
		return errors.New("--workers must be at least 1")
	}

	matcher, err := newExclusionMatcher(command.StringSlice("exclude"))
	if err != nil {
		return fmt.Errorf("invalid exclusion: %w", err)
	}

	started := time.Now()

	stats, err := copyPath(ctx, command.Args().Get(0), command.Args().Get(1), copyOptions{
		workers: workers,
		exclude: matcher,
	})

	if err != nil {
		return err
	}

	logger.Printf(
		"copied %d files, %d directories, %d symlinks, %s in %s\n",
		stats.files,
		stats.directories,
		stats.symlinks,
		formatBytes(stats.bytes),
		time.Since(started).Round(time.Millisecond),
	)

	return nil
}

func formatBytes(size int64) string {
	const unit = 1024

	if size < unit {
		return fmt.Sprintf("%d B", size)
	}

	var (
		divisor  = int64(unit)
		exponent int
	)

	for quotient := size / unit; quotient >= unit; quotient /= unit {
		divisor *= unit
		exponent++
	}

	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(divisor), "KMGTPE"[exponent])
}
