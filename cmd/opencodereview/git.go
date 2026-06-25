package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/open-code-review/open-code-review/internal/vcs"
)

func runGitCmd(repoDir string, args ...string) ([]byte, error) {
	fullArgs := append([]string{"-C", repoDir}, args...)
	cmd := exec.Command("git", fullArgs...)
	return cmd.CombinedOutput()
}

// getCommitMessage returns the log message for a commit (git) or revision
// (svn), used as background context for a single-commit review.
func getCommitMessage(repoDir string, kind vcs.Kind, commit string) (string, error) {
	if kind == vcs.SVN {
		return getSVNLogMessage(repoDir, commit)
	}
	out, err := runGitCmd(repoDir, "log", "-1", "--format=%B", "--end-of-options", commit)
	if err != nil {
		return "", fmt.Errorf("git log failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// getSVNLogMessage extracts just the commit message body for a single svn
// revision via `svn log -r REV -l 1`, stripping the dashed header/footer and
// the metadata line (rNNN | author | date | N line(s)).
func getSVNLogMessage(repoDir, rev string) (string, error) {
	cmd := exec.Command("svn", "log", "-r", rev, "-l", "1", repoDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("svn log failed: %w", err)
	}
	var body []string
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if strings.HasPrefix(trimmed, "-----") {
			continue
		}
		// Metadata line, e.g. "r793876 | author | 2026-... | 1 line".
		if strings.HasPrefix(trimmed, "r") && strings.Contains(trimmed, " | ") {
			continue
		}
		body = append(body, trimmed)
	}
	return strings.TrimSpace(strings.Join(body, "\n")), nil
}
