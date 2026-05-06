package docker

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDockerCandidatePathsIncludeDockerDesktopOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific Docker Desktop path")
	}
	paths := dockerCandidatePaths()
	found := false
	for _, path := range paths {
		if strings.Contains(strings.ToLower(filepath.ToSlash(path)), "docker/docker/resources/bin/docker.exe") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Docker Desktop CLI candidate, got %#v", paths)
	}
}

func TestUniqueStringsDropsDuplicatesAndEmptyValues(t *testing.T) {
	got := uniqueStrings([]string{"", "a", "a", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("unexpected unique values: %#v", got)
	}
}
