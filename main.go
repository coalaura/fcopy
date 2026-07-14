package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/coalaura/plain"
	"github.com/urfave/cli/v3"
)

type reportedError struct {
	error
}

type copyReport struct {
	FilesCopied         int64    `json:"files_copied"`
	DirectoriesCopied   int64    `json:"directories_copied"`
	SymlinksCopied      int64    `json:"symlinks_copied"`
	BytesCopied         int64    `json:"bytes_copied"`
	Exclusions          int64    `json:"exclusions"`
	CollisionsSkipped   int64    `json:"collisions_skipped"`
	ElapsedMilliseconds float64  `json:"elapsed_ms"`
	Errors              []string `json:"errors"`
}

var log = plain.New(plain.WithTarget(os.Stderr))

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
			&cli.StringSliceFlag{
				Name:  "exclude-name",
				Usage: "exclude a literal basename recursively; may be repeated",
			},
			&cli.BoolFlag{
				Name:  "dereference",
				Usage: "follow symbolic links and copy their targets",
			},
			&cli.StringFlag{
				Name:  "collision",
				Usage: "collision behavior: replace, warn, or fail",
				Value: "replace",
			},
			&cli.BoolFlag{
				Name:  "quiet",
				Usage: "suppress per-file output",
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "write machine-readable copy totals",
			},
			&cli.StringFlag{
				Name:  "reflink",
				Usage: "copy-on-write behavior: auto, always, or never",
				Value: "auto",
			},
		},
		Action: run,
	}

	err := command.Run(ctx, os.Args)
	if err != nil {
		if _, reported := errors.AsType[*reportedError](err); !reported {
			if errors.Is(err, context.Canceled) {
				log.Warnln("copy interrupted")
			} else {
				log.Errorf("fastcopy: %v\n", err)
			}
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

	matcher, err := newExclusionMatcher(command.StringSlice("exclude"), command.StringSlice("exclude-name"))
	if err != nil {
		return fmt.Errorf("invalid exclusion: %w", err)
	}

	collision, err := parseCollisionMode(command.String("collision"))
	if err != nil {
		return err
	}

	reflink, err := parseReflinkMode(command.String("reflink"))
	if err != nil {
		return err
	}

	jsonOutput := command.Bool("json")
	quiet := command.Bool("quiet")

	options := copyOptions{
		workers:     workers,
		exclude:     matcher,
		collision:   collision,
		reflink:     reflink,
		dereference: command.Bool("dereference"),
	}

	if !quiet && !jsonOutput {
		options.onCopied = func(source, destination string) {
			log.Printf("copied %q to %q\n", source, destination)
		}
	}

	if !jsonOutput {
		options.onWarning = func(message string) {
			log.Warnln(message)
		}
	}

	started := time.Now()

	stats, copyErr := copyPath(ctx, command.Args().Get(0), command.Args().Get(1), options)

	elapsed := time.Since(started)

	if jsonOutput {
		report := copyReport{
			FilesCopied:         stats.files,
			DirectoriesCopied:   stats.directories,
			SymlinksCopied:      stats.symlinks,
			BytesCopied:         stats.bytes,
			Exclusions:          stats.exclusions,
			CollisionsSkipped:   stats.collisionsSkipped,
			ElapsedMilliseconds: float64(elapsed) / float64(time.Millisecond),
			Errors:              []string{},
		}

		if copyErr != nil {
			report.Errors = append(report.Errors, copyErr.Error())
		}

		err = json.NewEncoder(os.Stdout).Encode(report)
		if err != nil {
			return fmt.Errorf("write JSON report: %w", err)
		}

		if copyErr != nil {
			return &reportedError{error: copyErr}
		}

		return nil
	}

	if copyErr != nil {
		return copyErr
	}

	log.Printf(
		"copied %d files, %d directories, %d symlinks, %s; excluded %d, skipped %d collisions in %s\n",
		stats.files,
		stats.directories,
		stats.symlinks,
		formatBytes(stats.bytes),
		stats.exclusions,
		stats.collisionsSkipped,
		elapsed.Round(time.Millisecond),
	)

	return nil
}

func parseCollisionMode(value string) (collisionMode, error) {
	switch value {
	case "replace":
		return collisionReplace, nil
	case "warn":
		return collisionWarn, nil
	case "fail":
		return collisionFail, nil
	default:
		return 0, fmt.Errorf("--collision must be replace, warn, or fail, not %q", value)
	}
}

func parseReflinkMode(value string) (reflinkMode, error) {
	switch value {
	case "auto":
		return reflinkAuto, nil
	case "always":
		return reflinkAlways, nil
	case "never":
		return reflinkNever, nil
	default:
		return 0, fmt.Errorf("--reflink must be auto, always, or never, not %q", value)
	}
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
