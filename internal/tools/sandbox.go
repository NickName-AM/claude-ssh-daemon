// Package tools — sandbox.go provides the withinBaseDir and withinLocalBaseDir
// helpers used by file-operation and exec handlers to enforce base_dir
// confinement on the remote side and local_base_dir confinement on the local side.
package tools

import (
	"path"
	"path/filepath"
	"strings"
)

// withinBaseDir reports whether requestedPath lexically resolves inside baseDir.
//
// Validation is purely lexical: path.Clean is applied to both arguments before
// comparison. Remote symlinks are NOT resolved — a symlink inside baseDir that
// points outside will pass this check. This limitation is documented in each
// affected tool's schema description, consistent with the SAFE-01 precedent
// (BDIR-03).
//
// Algorithm (D-05):
//  1. Clean both paths with path.Clean (POSIX — not filepath.Clean, D-06).
//  2. Normalise baseDir by stripping any trailing slash and appending one
//     fresh slash so that "/base" never falsely contains "/base_extra/file"
//     via a plain strings.HasPrefix check.
//  3. Return true if cleanedPath == cleanedBase OR cleanedPath starts with
//     cleanedBase+"/".
//
// Pre-condition: baseDir must be a non-empty absolute POSIX path.
// config.Validate() ensures this when BaseDir is set (BDIR-04).
func withinBaseDir(baseDir, requestedPath string) bool {
	cleanBase := path.Clean(baseDir)
	cleanPath := path.Clean(requestedPath)

	// Exact match — e.g. the path IS the base directory itself.
	if cleanPath == cleanBase {
		return true
	}

	// Special case: baseDir is root ("/"). path.Clean("/") returns "/" and appending
	// "/" would form "//" which no cleaned path starts with. Any absolute path
	// (always starting with "/") is contained by root.
	if cleanBase == "/" {
		return strings.HasPrefix(cleanPath, "/")
	}

	// Prefix match with trailing-slash boundary to prevent false positives
	// like /base matching /base_extra (D-05).
	return strings.HasPrefix(cleanPath, cleanBase+"/")
}

// withinLocalBaseDir reports whether requestedPath lexically resolves inside
// baseDir, using LOCAL (OS) path semantics.
//
// Why this exists separately from withinBaseDir: withinBaseDir is deliberately
// POSIX-only (path.Clean, "/" separators, D-06) because it validates paths on
// the REMOTE host, which is always POSIX regardless of where the daemon runs.
// local_base_dir constrains paths on the daemon's own filesystem, so it must use
// path/filepath (OS-native separators and volume handling). Keeping the two
// helpers apart avoids changing remote validation semantics on any platform.
//
// Validation is purely lexical: local symlinks are NOT resolved, so a symlink
// inside baseDir that points outside will pass this check — same limitation and
// rationale as withinBaseDir (BDIR-03).
//
// Pre-condition: baseDir must be a non-empty absolute path.
// config.Validate() ensures this when LocalBaseDir is set.
func withinLocalBaseDir(baseDir, requestedPath string) bool {
	cleanBase := filepath.Clean(baseDir)
	cleanPath := filepath.Clean(requestedPath)

	// Exact match — e.g. the path IS the base directory itself.
	if cleanPath == cleanBase {
		return true
	}

	// Special case: baseDir is the filesystem root. filepath.Clean leaves the
	// separator in place and appending another would form "//", which no cleaned
	// path starts with. Any path under root is contained by it.
	sep := string(filepath.Separator)
	if strings.HasSuffix(cleanBase, sep) {
		return strings.HasPrefix(cleanPath, cleanBase)
	}

	// Prefix match with trailing-separator boundary to prevent false positives
	// like /base matching /base_extra (same boundary rule as withinBaseDir).
	return strings.HasPrefix(cleanPath, cleanBase+sep)
}
