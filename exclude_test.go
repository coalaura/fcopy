package main

import "testing"

func TestExclusionMatcher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		patterns []string
		path     string
		want     bool
	}{
		{
			name:     "base name in root",
			patterns: []string{"*.tmp"},
			path:     "cache.tmp",
			want:     true,
		},
		{
			name:     "base name nested",
			patterns: []string{"*.tmp"},
			path:     "data/cache.tmp",
			want:     true,
		},
		{
			name:     "directory base name",
			patterns: []string{".git"},
			path:     "source/.git",
			want:     true,
		},
		{
			name:     "single path segment",
			patterns: []string{"build/*.o"},
			path:     "build/main.o",
			want:     true,
		},
		{
			name:     "single segment does not cross directory",
			patterns: []string{"build/*.o"},
			path:     "build/debug/main.o",
			want:     false,
		},
		{
			name:     "recursive wildcard",
			patterns: []string{"build/**/*.o"},
			path:     "build/debug/native/main.o",
			want:     true,
		},
		{
			name:     "recursive wildcard matches zero segments",
			patterns: []string{"build/**/*.o"},
			path:     "build/main.o",
			want:     true,
		},
		{
			name:     "unmatched",
			patterns: []string{"*.tmp", "build/**"},
			path:     "source/main.go",
			want:     false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			matcher, err := newExclusionMatcher(test.patterns, nil)
			if err != nil {
				t.Fatalf("newExclusionMatcher() error = %v", err)
			}

			got := matcher.matches(test.path)
			if got != test.want {
				t.Errorf("matches(%q) = %v, want %v", test.path, got, test.want)
			}
		})
	}
}

func TestExclusionMatcherName(t *testing.T) {
	t.Parallel()

	matcher, err := newExclusionMatcher(nil, []string{"generated.go"})
	if err != nil {
		t.Fatalf("newExclusionMatcher() error = %v", err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "root",
			path: "generated.go",
			want: true,
		},
		{
			name: "nested",
			path: "internal/api/generated.go",
			want: true,
		},
		{
			name: "literal does not use glob syntax",
			path: "generatedXgo",
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := matcher.matches(test.path)
			if got != test.want {
				t.Errorf("matches(%q) = %v, want %v", test.path, got, test.want)
			}
		})
	}
}
