// Package tools — sandbox.go provides the withinBaseDir helper used by all
// file-operation and exec handlers to enforce per-host base_dir confinement.
package tools

import (
	"path"
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
