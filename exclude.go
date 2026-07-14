package main

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

type exclusionMatcher struct {
	patterns []exclusionPattern
	names    map[string]struct{}
}

type exclusionPattern struct {
	segments     []string
	matchBase    bool
	hasRecursive bool
}

func newExclusionMatcher(patterns []string, names []string) (*exclusionMatcher, error) {
	matcher := &exclusionMatcher{
		patterns: make([]exclusionPattern, 0, len(patterns)),
		names:    make(map[string]struct{}, len(names)),
	}

	for _, name := range names {
		if name == "" {
			return nil, errors.New("exclude name must not be empty")
		}

		if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
			return nil, fmt.Errorf("%q is not a basename", name)
		}

		matcher.names[name] = struct{}{}
	}

	for _, pattern := range patterns {
		normalized := strings.ReplaceAll(pattern, `\`, "/")
		normalized = strings.TrimPrefix(normalized, "./")
		normalized = strings.TrimPrefix(normalized, "/")
		normalized = strings.TrimSuffix(normalized, "/")

		if normalized == "" || normalized == "." {
			return nil, fmt.Errorf("%q excludes the entire source", pattern)
		}

		segments := strings.Split(normalized, "/")
		exclusion := exclusionPattern{
			segments:  segments,
			matchBase: len(segments) == 1,
		}

		for _, segment := range segments {
			if segment == "**" {
				exclusion.hasRecursive = true

				continue
			}

			_, err := path.Match(segment, "validation")
			if err != nil {
				return nil, fmt.Errorf("%q: %w", pattern, err)
			}
		}

		matcher.patterns = append(matcher.patterns, exclusion)
	}

	return matcher, nil
}

func (matcher *exclusionMatcher) matches(relativePath string) bool {
	if matcher == nil {
		return false
	}

	normalized := filepath.ToSlash(relativePath)
	pathSegments := strings.Split(normalized, "/")
	baseName := pathSegments[len(pathSegments)-1]

	if _, excluded := matcher.names[baseName]; excluded {
		return true
	}

	for _, pattern := range matcher.patterns {
		if pattern.matchBase {
			matched, _ := path.Match(pattern.segments[0], pathSegments[len(pathSegments)-1])
			if matched {
				return true
			}

			continue
		}

		if pattern.matches(pathSegments) {
			return true
		}
	}

	return false
}

func (pattern exclusionPattern) matches(pathSegments []string) bool {
	if !pattern.hasRecursive {
		if len(pattern.segments) != len(pathSegments) {
			return false
		}

		for i, segmentPattern := range pattern.segments {
			matched, _ := path.Match(segmentPattern, pathSegments[i])
			if !matched {
				return false
			}
		}

		return true
	}

	return matchRecursiveSegments(pattern.segments, pathSegments)
}

func matchRecursiveSegments(patterns, names []string) bool {
	for len(patterns) > 0 {
		if patterns[0] != "**" {
			if len(names) == 0 {
				return false
			}

			matched, _ := path.Match(patterns[0], names[0])
			if !matched {
				return false
			}

			patterns = patterns[1:]
			names = names[1:]

			continue
		}

		for len(patterns) > 1 && patterns[1] == "**" {
			patterns = patterns[1:]
		}

		if len(patterns) == 1 {
			return true
		}

		for i := range len(names) + 1 {
			if matchRecursiveSegments(patterns[1:], names[i:]) {
				return true
			}
		}

		return false
	}

	return len(names) == 0
}
