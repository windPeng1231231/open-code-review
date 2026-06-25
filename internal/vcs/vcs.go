// Package vcs abstracts the version-control operations open-code-review needs
// so it can review both git and Subversion (svn) working copies.
//
// It owns three concerns:
//   - repository-kind detection (Detect) and the --vcs flag parsing (ParseKind),
//   - svn revision validation (ValidRevision) mirroring the git ref-injection
//     guard used on the git path,
//   - the svn→git unified-diff normalization (NormalizeSVNDiff, see svndiff.go)
//     that lets internal/diff's existing parser stay VCS-agnostic.
//
// The package intentionally depends only on the standard library so it can be
// imported by internal/diff, internal/tool and cmd without import cycles.
package vcs

import (
	"os/exec"
	"strings"
)

// Kind identifies the version-control system backing a working directory.
// The zero value is None; callers that default to git should treat
// None == Git for read paths.
type Kind int

const (
	// None means no VCS was detected (or detection was not run).
	None Kind = iota
	// Git working copy.
	Git
	// SVN (Subversion) working copy.
	SVN
)

// String returns the lowercase backend name ("git", "svn", "none").
func (k Kind) String() string {
	switch k {
	case Git:
		return "git"
	case SVN:
		return "svn"
	default:
		return "none"
	}
}

// Binary returns the executable name used to drive this VCS. None falls back
// to "git" so a shared gitcmd.Runner created for a non-VCS scan directory has
// a harmless default.
func (k Kind) Binary() string {
	if k == SVN {
		return "svn"
	}
	return "git"
}

// ParseKind converts a --vcs flag value into a Kind. "auto" / "" yields
// (None, true) — the caller should then run Detect. Unknown values return
// (None, false).
func ParseKind(s string) (Kind, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return None, true
	case "git":
		return Git, true
	case "svn":
		return SVN, true
	default:
		return None, false
	}
}

// Detect probes dir and reports which VCS backs it. Git is checked first
// (cheap and most common); svn second. Returns None when neither is found or
// the corresponding client binary is unavailable.
func Detect(dir string) Kind {
	if isGitRepo(dir) {
		return Git
	}
	if isSVNRepo(dir) {
		return SVN
	}
	return None
}

func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

func isSVNRepo(dir string) bool {
	// `svn info <dir>` exits 0 from anywhere inside a working copy (including
	// nested subdirectories, where a stat of ".svn" would miss in svn 1.7+).
	cmd := exec.Command("svn", "info", dir)
	return cmd.Run() == nil
}

// svnKeywordRevisions are the symbolic revisions svn accepts in place of a
// numeric revision.
var svnKeywordRevisions = map[string]struct{}{
	"HEAD":      {},
	"BASE":      {},
	"COMMITTED": {},
	"PREV":      {},
}

// ValidRevision reports whether s is a syntactically valid svn revision
// argument: a positive integer, a known keyword (HEAD/BASE/COMMITTED/PREV),
// or a {DATE} expression. It rejects empty and option-like values (leading
// '-') to mirror the git ref-injection guard (#112). It does NOT contact the
// repository — existence is validated by the actual svn command later.
func ValidRevision(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") {
		return false
	}
	if _, ok := svnKeywordRevisions[strings.ToUpper(s)]; ok {
		return true
	}
	// {DATE} form, e.g. {2026-06-01} or {"2026-06-01 12:00"}.
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") && len(s) > 2 {
		return true
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
