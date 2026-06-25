package vcs

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The fixtures below are real `svn diff` fragments captured from a Subversion
// working copy (cwd = working-copy root, plain `svn diff`, WC-relative paths).

func TestNormalizeSVNDiff_Modified(t *testing.T) {
	raw := "Index: .codex/config.toml\n" +
		"===================================================================\n" +
		"--- .codex/config.toml\t(revision 793876)\n" +
		"+++ .codex/config.toml\t(working copy)\n" +
		"@@ -24,3 +24,5 @@\n" +
		" [mcp_servers.browser-use]\n" +
		" command = \"browser-use\"\n" +
		" args = [\"--mcp\", \"--headed\"]\n" +
		"+\n" +
		"+[mcp_servers.headroom]\n"

	got := NormalizeSVNDiff(raw)

	if !strings.Contains(got, "diff --git a/.codex/config.toml b/.codex/config.toml\n") {
		t.Errorf("missing git header:\n%s", got)
	}
	if !strings.Contains(got, "--- a/.codex/config.toml\n") || !strings.Contains(got, "+++ b/.codex/config.toml\n") {
		t.Errorf("modified file should keep a/ b/ markers:\n%s", got)
	}
	if strings.Contains(got, "new file mode") || strings.Contains(got, "deleted file mode") {
		t.Errorf("modified file must not be flagged new/deleted:\n%s", got)
	}
	if !strings.Contains(got, "@@ -24,3 +24,5 @@") {
		t.Errorf("hunk header lost:\n%s", got)
	}
	if !strings.Contains(got, "+[mcp_servers.headroom]") {
		t.Errorf("added line lost:\n%s", got)
	}
}

func TestNormalizeSVNDiff_NewFile(t *testing.T) {
	raw := "Index: .emmyrc.json\n" +
		"===================================================================\n" +
		"--- .emmyrc.json\t(nonexistent)\n" +
		"+++ .emmyrc.json\t(working copy)\n" +
		"@@ -0,0 +1,2 @@\n" +
		"+{\n" +
		"+    \"$schema\": \"vscode://schemas/emmylua\"\n"

	got := NormalizeSVNDiff(raw)

	if !strings.Contains(got, "diff --git a/.emmyrc.json b/.emmyrc.json\n") {
		t.Errorf("missing git header:\n%s", got)
	}
	if !strings.Contains(got, "new file mode 100644\n") {
		t.Errorf("new file must carry new-file-mode header:\n%s", got)
	}
	if !strings.Contains(got, "--- /dev/null\n") || !strings.Contains(got, "+++ b/.emmyrc.json\n") {
		t.Errorf("new file should map old side to /dev/null:\n%s", got)
	}
	if !strings.Contains(got, "+    \"$schema\"") {
		t.Errorf("added content lost:\n%s", got)
	}
}

func TestNormalizeSVNDiff_DeletedFile(t *testing.T) {
	raw := "Index: Assets/Art/Effects/models/Special/unit_6551_low_dan.FBX\n" +
		"===================================================================\n" +
		"--- Assets/Art/Effects/models/Special/unit_6551_low_dan.FBX\t(revision 793876)\n" +
		"+++ Assets/Art/Effects/models/Special/unit_6551_low_dan.FBX\t(nonexistent)\n" +
		"@@ -1,2 +0,0 @@\n" +
		"-; FBX 7.5.0 project file\n" +
		"-line two\n"

	got := NormalizeSVNDiff(raw)

	if !strings.Contains(got, "deleted file mode 100644\n") {
		t.Errorf("deleted file must carry deleted-file-mode header:\n%s", got)
	}
	if !strings.Contains(got, "--- a/Assets/Art/Effects/models/Special/unit_6551_low_dan.FBX\n") {
		t.Errorf("deleted file should keep old a/ side:\n%s", got)
	}
	if !strings.Contains(got, "+++ /dev/null\n") {
		t.Errorf("deleted file should map new side to /dev/null:\n%s", got)
	}
}

func TestNormalizeSVNDiff_Binary(t *testing.T) {
	raw := "Index: Assets/Art/Effects/CommonTexture/ui/fx.png\n" +
		"===================================================================\n" +
		"Cannot display: file marked as a binary type.\n" +
		"svn:mime-type = application/octet-stream\n"

	got := NormalizeSVNDiff(raw)

	if !strings.Contains(got, "diff --git a/Assets/Art/Effects/CommonTexture/ui/fx.png b/Assets/Art/Effects/CommonTexture/ui/fx.png\n") {
		t.Errorf("binary file missing git header:\n%s", got)
	}
	if !strings.Contains(got, "Binary files ") || !strings.Contains(got, " differ\n") {
		t.Errorf("binary file must emit a Binary-files marker:\n%s", got)
	}
}

func TestNormalizeSVNDiff_PropertyOnlyDropped(t *testing.T) {
	raw := "Index: somefile\n" +
		"===================================================================\n" +
		"--- somefile\t(revision 100)\n" +
		"+++ somefile\t(working copy)\n" +
		"\n" +
		"Property changes on: somefile\n" +
		"___________________________________________________________________\n" +
		"Added: svn:executable\n" +
		"## -0,0 +1 ##\n" +
		"+*\n"

	got := NormalizeSVNDiff(raw)
	if strings.TrimSpace(got) != "" {
		t.Errorf("property-only change should produce no diff, got:\n%s", got)
	}
}

func TestNormalizeSVNDiff_TrailingPropertyBlockStripped(t *testing.T) {
	raw := "Index: file.go\n" +
		"===================================================================\n" +
		"--- file.go\t(revision 100)\n" +
		"+++ file.go\t(working copy)\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-old\n" +
		"+new\n" +
		"\n" +
		"Property changes on: file.go\n" +
		"___________________________________________________________________\n" +
		"Added: svn:keywords\n"

	got := NormalizeSVNDiff(raw)
	if strings.Contains(got, "Property changes on:") || strings.Contains(got, "svn:keywords") {
		t.Errorf("trailing property block must be stripped:\n%s", got)
	}
	if !strings.Contains(got, "+new") || !strings.Contains(got, "-old") {
		t.Errorf("textual hunk must be preserved:\n%s", got)
	}
}

func TestNormalizeSVNDiff_MultipleFiles(t *testing.T) {
	raw := "Index: a.txt\n" +
		"===================================================================\n" +
		"--- a.txt\t(revision 1)\n" +
		"+++ a.txt\t(working copy)\n" +
		"@@ -1 +1 @@\n" +
		"-a\n" +
		"+A\n" +
		"Index: b.txt\n" +
		"===================================================================\n" +
		"--- b.txt\t(nonexistent)\n" +
		"+++ b.txt\t(working copy)\n" +
		"@@ -0,0 +1 @@\n" +
		"+B\n"

	got := NormalizeSVNDiff(raw)
	if strings.Count(got, "diff --git ") != 2 {
		t.Errorf("expected 2 file headers, got:\n%s", got)
	}
	if !strings.Contains(got, "diff --git a/a.txt b/a.txt") || !strings.Contains(got, "diff --git a/b.txt b/b.txt") {
		t.Errorf("both files must be present:\n%s", got)
	}
}

func TestNormalizeSVNDiffPrefixed(t *testing.T) {
	raw := "Index: foo/bar.cs\n" +
		"===================================================================\n" +
		"--- foo/bar.cs\t(revision 1)\n" +
		"+++ foo/bar.cs\t(working copy)\n" +
		"@@ -1 +1 @@\n" +
		"-a\n" +
		"+b\n"

	got := NormalizeSVNDiffPrefixed(raw, "Assets/Tools")
	if !strings.Contains(got, "diff --git a/Assets/Tools/foo/bar.cs b/Assets/Tools/foo/bar.cs") {
		t.Errorf("path not prefixed with external subdir:\n%s", got)
	}
	if !strings.Contains(got, "--- a/Assets/Tools/foo/bar.cs") || !strings.Contains(got, "+++ b/Assets/Tools/foo/bar.cs") {
		t.Errorf("marker paths not prefixed:\n%s", got)
	}

	// Empty / slash-only prefix behaves like the plain normalizer.
	plain := NormalizeSVNDiff(raw)
	if NormalizeSVNDiffPrefixed(raw, "") != plain || NormalizeSVNDiffPrefixed(raw, "/") != plain {
		t.Errorf("empty prefix should equal plain normalization")
	}
}

func TestDiscoverNestedWorkingCopies(t *testing.T) {
	root := t.TempDir()
	// Layout:
	//   root/.svn                         (root WC, excluded from results)
	//   root/Assets/StreamingAssets/.svn  (nested, depth 2)
	//   root/res/Android/.svn             (nested, depth 2)
	//   root/node_modules/pkg/.svn        (excluded dir, must be skipped)
	//   root/Assets/Art/...               (deep non-WC noise)
	mkWC := func(parts ...string) {
		p := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(filepath.Join(p, ".svn"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mkWC()                               // root .svn
	mkWC("Assets", "StreamingAssets")    // nested
	mkWC("res", "Android")               // nested
	mkWC("node_modules", "pkg")          // excluded
	if err := os.MkdirAll(filepath.Join(root, "Assets", "Art", "deep", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := DiscoverNestedWorkingCopies(root, 4)
	sort.Strings(got)
	want := []string{"Assets/StreamingAssets", "res/Android"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("DiscoverNestedWorkingCopies = %v, want %v", got, want)
	}

	if DiscoverNestedWorkingCopies(root, 0) != nil {
		t.Errorf("maxDepth<=0 must return nil (aggregation disabled)")
	}
	// Depth 1 is too shallow to reach the depth-2 nested copies.
	if len(DiscoverNestedWorkingCopies(root, 1)) != 0 {
		t.Errorf("depth 1 should find nothing for depth-2 nested copies")
	}
}

func TestValidRevision(t *testing.T) {
	valid := []string{"1", "793876", "HEAD", "head", "BASE", "PREV", "COMMITTED", "{2026-06-01}"}
	for _, v := range valid {
		if !ValidRevision(v) {
			t.Errorf("ValidRevision(%q) = false, want true", v)
		}
	}
	invalid := []string{"", "-r", "--foo", "abc", "12a", "HEAD~1", "deadbeef"}
	for _, v := range invalid {
		if ValidRevision(v) {
			t.Errorf("ValidRevision(%q) = true, want false", v)
		}
	}
}

func TestParseKind(t *testing.T) {
	cases := map[string]struct {
		want Kind
		ok   bool
	}{
		"":     {None, true},
		"auto": {None, true},
		"git":  {Git, true},
		"svn":  {SVN, true},
		"SVN":  {SVN, true},
		"hg":   {None, false},
	}
	for in, exp := range cases {
		got, ok := ParseKind(in)
		if got != exp.want || ok != exp.ok {
			t.Errorf("ParseKind(%q) = (%v,%v), want (%v,%v)", in, got, ok, exp.want, exp.ok)
		}
	}
}
