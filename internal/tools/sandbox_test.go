package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWithinBaseDirExactMatch verifies that a path equal to baseDir returns true.
// The base directory itself is a valid location for file operations.
func TestWithinBaseDirExactMatch(t *testing.T) {
	require.True(t, withinBaseDir("/home/ubuntu/project", "/home/ubuntu/project"),
		"exact base_dir path must be within base_dir")
}

// TestWithinBaseDirSubpath verifies that a clean path nested inside baseDir returns true.
func TestWithinBaseDirSubpath(t *testing.T) {
	require.True(t, withinBaseDir("/home/ubuntu/project", "/home/ubuntu/project/src/main.go"),
		"nested path must be within base_dir")
}

// TestWithinBaseDirSubdir verifies that a subdirectory of baseDir returns true.
func TestWithinBaseDirSubdir(t *testing.T) {
	require.True(t, withinBaseDir("/home/ubuntu/project", "/home/ubuntu/project/internal/tools"),
		"subdirectory must be within base_dir")
}

// TestWithinBaseDirSiblingDirectory verifies that a directory with the same prefix
// but a longer name (e.g. /base_extra) is correctly rejected (D-05 boundary check).
func TestWithinBaseDirSiblingDirectory(t *testing.T) {
	require.False(t, withinBaseDir("/base", "/base_extra/file"),
		"/base must not contain /base_extra/file — trailing slash prevents false prefix match (D-05)")
}

// TestWithinBaseDirSiblingFile verifies that a file at the same level as baseDir
// (e.g. /home/ubuntu/project.bak) is rejected.
func TestWithinBaseDirSiblingFile(t *testing.T) {
	require.False(t, withinBaseDir("/home/ubuntu/project", "/home/ubuntu/project.bak"),
		"sibling path with same prefix must not be within base_dir")
}

// TestWithinBaseDirParentDir verifies that the parent of baseDir is rejected.
func TestWithinBaseDirParentDir(t *testing.T) {
	require.False(t, withinBaseDir("/home/ubuntu/project", "/home/ubuntu"),
		"parent directory must not be within base_dir")
}

// TestWithinBaseDirRootDir verifies that "/" is rejected when baseDir is a subdirectory.
func TestWithinBaseDirRootDir(t *testing.T) {
	require.False(t, withinBaseDir("/home/ubuntu/project", "/"),
		"root directory must not be within base_dir")
}

// TestWithinBaseDirAbsoluteEscape verifies that an absolute path outside baseDir
// is rejected (e.g. /etc/passwd when base_dir is /home/ubuntu/project).
func TestWithinBaseDirAbsoluteEscape(t *testing.T) {
	require.False(t, withinBaseDir("/home/ubuntu/project", "/etc/passwd"),
		"absolute path outside base_dir must be rejected (BDIR-01)")
}

// TestWithinBaseDirTraversalSequenceCleanedToEscape verifies that a path containing
// "../" sequences that resolve outside baseDir is rejected after path.Clean (BDIR-01).
func TestWithinBaseDirTraversalSequenceCleanedToEscape(t *testing.T) {
	// /home/ubuntu/project/../../../etc/passwd cleans to /etc/passwd
	require.False(t, withinBaseDir("/home/ubuntu/project", "/home/ubuntu/project/../../../etc/passwd"),
		"traversal sequence resolving outside base_dir must be rejected (BDIR-01)")
}

// TestWithinBaseDirTraversalSequenceCleanedToInside verifies that a path with "../"
// sequences that ultimately resolves inside baseDir is accepted.
func TestWithinBaseDirTraversalSequenceCleanedToInside(t *testing.T) {
	// /home/ubuntu/project/src/../lib cleans to /home/ubuntu/project/lib
	require.True(t, withinBaseDir("/home/ubuntu/project", "/home/ubuntu/project/src/../lib"),
		"traversal sequence resolving inside base_dir must be accepted")
}

// TestWithinBaseDirTrailingSlashOnBaseDir verifies that a trailing slash on baseDir
// is handled correctly — the result is identical to the no-trailing-slash form.
func TestWithinBaseDirTrailingSlashOnBaseDir(t *testing.T) {
	// config.Validate() cleans baseDir at load time (filepath.Clean strips trailing
	// slashes), but withinBaseDir must be robust even if called with a raw value.
	require.True(t, withinBaseDir("/home/ubuntu/project/", "/home/ubuntu/project/src/main.go"),
		"trailing slash on baseDir must not break containment check")
	require.False(t, withinBaseDir("/home/ubuntu/project/", "/home/ubuntu/other/file"),
		"trailing slash on baseDir must not create false positives")
}

// TestWithinBaseDirTrailingSlashOnRequestedPath verifies that a trailing slash on
// requestedPath is handled correctly via path.Clean normalisation.
func TestWithinBaseDirTrailingSlashOnRequestedPath(t *testing.T) {
	require.True(t, withinBaseDir("/home/ubuntu/project", "/home/ubuntu/project/src/"),
		"trailing slash on requestedPath must not break containment check")
}

// TestWithinBaseDirRootBaseDir verifies that baseDir="/" contains any absolute path.
// This is a degenerate case (no practical restriction) but must not panic.
func TestWithinBaseDirRootBaseDir(t *testing.T) {
	require.True(t, withinBaseDir("/", "/etc/passwd"),
		"base_dir=/ must contain any absolute path")
	require.True(t, withinBaseDir("/", "/"),
		"base_dir=/ must contain / itself")
}

// TestWithinBaseDirDotDotAtRoot verifies that traversal from "/" doesn't escape.
// path.Clean("/../etc") → "/etc", which is within "/".
func TestWithinBaseDirDotDotAtRoot(t *testing.T) {
	require.True(t, withinBaseDir("/", "/../etc"),
		"traversal from root cleans to /etc, which is within /")
}

// TestWithinBaseDirDeepNested verifies a deeply nested path is accepted.
func TestWithinBaseDirDeepNested(t *testing.T) {
	require.True(t, withinBaseDir("/var/app", "/var/app/data/cache/2026/06/file.json"),
		"deeply nested path must be within base_dir")
}
