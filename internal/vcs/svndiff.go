package vcs

import (
	"os"
	"path/filepath"
	"strings"
)

// NormalizeSVNDiff rewrites the output of a plain `svn diff` into the git
// unified-diff form that internal/diff.ParseDiffText understands, so the
// existing parser (and everything downstream) stays VCS-agnostic.
//
// IMPORTANT: feed it the output of a plain `svn diff` run with the working
// copy as the process CWD — NOT `svn diff --git`. Plain svn diff emits
// working-copy-relative paths in its "Index:" / "---" / "+++" lines, which
// is what lets downstream content resolution read files straight off disk.
// `svn diff --git` instead emits repository-root-relative paths that would
// not exist on disk under the working-copy root.
//
// Per file block (delimited by "Index: <path>"):
//   - "--- p (nonexistent|revision 0)" + "+++ p (working copy)" → new file
//   - "--- p (revision N)" + "+++ p (nonexistent)"             → deleted file
//   - otherwise                                                → modified file
//   - "Cannot display: file marked as a binary type."          → Binary marker
//   - "Property changes on: p" blocks (svn-only metadata)      → dropped
//
// Paths are normalized to forward slashes for platform-independent downstream
// resolution.
func NormalizeSVNDiff(raw string) string {
	return NormalizeSVNDiffPrefixed(raw, "")
}

// NormalizeSVNDiffPrefixed is NormalizeSVNDiff with an extra path prefix
// prepended to every file path. It is used when aggregating the diff of a
// nested working copy (svn external) into a parent review: prefix is the
// nested working copy's path relative to the parent root (forward slashes,
// no trailing slash), e.g. "Assets/Tools". An empty prefix is a no-op and
// behaves exactly like NormalizeSVNDiff.
func NormalizeSVNDiffPrefixed(raw, prefix string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	prefix = strings.Trim(filepath.ToSlash(prefix), "/")
	lines := strings.Split(raw, "\n")
	var out strings.Builder

	i := 0
	for i < len(lines) {
		if !strings.HasPrefix(lines[i], "Index: ") {
			// Drop anything before the first Index: header (svn summary or
			// blank lines) and any stray content between blocks.
			i++
			continue
		}
		path := filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(lines[i], "Index: ")))
		if prefix != "" {
			path = prefix + "/" + path
		}
		i++

		// Gather this file's block until the next "Index: " header.
		var block []string
		for i < len(lines) && !strings.HasPrefix(lines[i], "Index: ") {
			block = append(block, lines[i])
			i++
		}
		out.WriteString(normalizeSVNBlock(path, block))
	}
	return out.String()
}

// normalizeSVNBlock converts a single svn file block into git form. Returns
// "" for blocks with no reviewable textual change (e.g. property-only diffs).
func normalizeSVNBlock(path string, block []string) string {
	// Binary files: svn emits a "Cannot display" notice with no hunks.
	for _, l := range block {
		if strings.HasPrefix(l, "Cannot display:") {
			return "diff --git a/" + path + " b/" + path + "\n" +
				"Binary files a/" + path + " and b/" + path + " differ\n"
		}
	}

	// Locate the --- / +++ header lines and the first hunk.
	var oldMarker, newMarker string
	hunkStart := -1
	for idx, l := range block {
		switch {
		case strings.HasPrefix(l, "--- "):
			oldMarker = l
		case strings.HasPrefix(l, "+++ "):
			newMarker = l
		case strings.HasPrefix(l, "@@"):
			hunkStart = idx
		}
		if hunkStart >= 0 {
			break
		}
	}

	// No hunk → property-only change or an empty block: nothing to review.
	if hunkStart < 0 {
		return ""
	}

	isNew := strings.Contains(oldMarker, "(nonexistent)") || strings.Contains(oldMarker, "(revision 0)")
	isDeleted := strings.Contains(newMarker, "(nonexistent)")

	var sb strings.Builder
	sb.WriteString("diff --git a/" + path + " b/" + path + "\n")
	switch {
	case isNew:
		sb.WriteString("new file mode 100644\n")
		sb.WriteString("--- /dev/null\n")
		sb.WriteString("+++ b/" + path + "\n")
	case isDeleted:
		sb.WriteString("deleted file mode 100644\n")
		sb.WriteString("--- a/" + path + "\n")
		sb.WriteString("+++ /dev/null\n")
	default:
		sb.WriteString("--- a/" + path + "\n")
		sb.WriteString("+++ b/" + path + "\n")
	}

	// Emit hunk content, stopping at a trailing "Property changes on:" block
	// which svn appends after the textual diff of a file.
	for _, l := range block[hunkStart:] {
		if strings.HasPrefix(l, "Property changes on:") {
			break
		}
		sb.WriteString(l)
		sb.WriteString("\n")
	}
	return sb.String()
}

// DefaultExternalsDepth is the default maximum directory depth scanned when
// discovering nested svn working copies (svn externals / separate checkouts).
// Externals are almost always shallow; capping the scan keeps discovery fast
// on very large working copies.
const DefaultExternalsDepth = 4

// discoveryExcludedDirs are directory names never descended into while looking
// for nested working copies. Kept local to avoid importing internal/diff
// (which would create an import cycle).
var discoveryExcludedDirs = map[string]struct{}{
	".svn": {}, ".git": {}, ".idea": {}, ".vscode": {},
	"node_modules": {}, "vendor": {}, "target": {},
	".happypack": {}, ".cachefile": {}, "_packages": {},
}

// DiscoverNestedWorkingCopies returns the paths (relative to root, forward
// slashes) of nested svn working copies beneath root — i.e. svn externals or
// separately checked-out working copies, each identified by its own ".svn"
// directory. root itself is excluded. Scanning is bounded to maxDepth levels
// (maxDepth<=0 returns nil) and skips well-known noise directories.
//
// Discovery is by .svn presence rather than parsing svn:externals because the
// latter is unreliable: externals defined outside the root's own tree (e.g.
// sibling-path checkouts) do not appear in `svn propget -R svn:externals`.
func DiscoverNestedWorkingCopies(root string, maxDepth int) []string {
	if maxDepth <= 0 {
		return nil
	}
	var found []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth >= maxDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if _, skip := discoveryExcludedDirs[name]; skip {
				continue
			}
			sub := filepath.Join(dir, name)
			if isSVNWorkingCopyRoot(sub) {
				if rel, rerr := filepath.Rel(root, sub); rerr == nil {
					found = append(found, filepath.ToSlash(rel))
				}
				// Do not descend into a discovered working copy: nested
				// externals-of-externals are rare and would be reviewed when
				// that copy is targeted directly.
				continue
			}
			walk(sub, depth+1)
		}
	}
	walk(root, 0)
	return found
}

// isSVNWorkingCopyRoot reports whether dir contains a ".svn" directory,
// i.e. it is the root of an svn working copy (svn 1.7+ keeps a single .svn
// at each working-copy root).
func isSVNWorkingCopyRoot(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".svn"))
	return err == nil && info.IsDir()
}
