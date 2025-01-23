package diffcoverage

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

// TestRunDiffCoverage ensures every branch in RunDiffCoverage is covered.
func TestRunDiffCoverage(t *testing.T) {
	t.Run("parseGoMod error", func(t *testing.T) {
		// Pass sourceRoot that doesn't exist => parseGoMod fails => returns error
		coverPercent, uncovered, err := RunDiffCoverage("fakeCover.out", "fakeDiff.diff", "/non/existent", 0.0)
		if err == nil {
			t.Fatalf("Expected parseGoMod error, got nil")
		}
		if coverPercent != 0 || uncovered != nil {
			t.Errorf("Expected (0, nil) on parseGoMod error, got (%.2f, %#v)", coverPercent, uncovered)
		}
	})

	t.Run("parseCoverFile error", func(t *testing.T) {
		// Make a valid go.mod so parseGoMod succeeds,
		// but pass a coverPath that doesn't exist => parseCoverFile fails
		tmpDir := t.TempDir()
		writeGoMod(t, tmpDir, "github.com/example/module")

		coverPercent, uncovered, err := RunDiffCoverage(filepath.Join(tmpDir, "no_such_file.out"), "fakeDiff.diff", tmpDir, 0.0)
		if err == nil {
			t.Fatalf("Expected parseCoverFile error, got nil")
		}
		if coverPercent != 0 || uncovered != nil {
			t.Errorf("Expected (0, nil), got (%.2f, %#v)", coverPercent, uncovered)
		}
	})

	t.Run("parseDiffFile error", func(t *testing.T) {
		// Valid go.mod + cover.out, but bogus diff => parseDiffFile fails
		tmpDir := t.TempDir()
		writeGoMod(t, tmpDir, "github.com/example/module")
		writeCoverFile(t, tmpDir, "cover.out", `mode: set
github.com/example/module/pkg/foo.go:3.0,3.10 1 1
`)

		coverPercent, uncovered, err := RunDiffCoverage(filepath.Join(tmpDir, "cover.out"), filepath.Join(tmpDir, "no_such_diff.diff"), tmpDir, 0.0)
		if err == nil {
			t.Fatalf("Expected parseDiffFile error, got nil")
		}
		if coverPercent != 0 || uncovered != nil {
			t.Errorf("Expected (0, nil), got (%.2f, %#v)", coverPercent, uncovered)
		}
	})

	t.Run("No new/changed Go files => len(filesToAnalyze)==0", func(t *testing.T) {
		// Valid go.mod + cover.out + diff with no .go lines => no files to analyze
		tmpDir := t.TempDir()
		writeGoMod(t, tmpDir, "github.com/example/module")

		writeCoverFile(t, tmpDir, "cover.out", `mode: set
github.com/example/module/pkg/foo.go:3.0,3.10 1 1
`)

		// Diff that references only .md, never .go
		writeDiffFile(t, tmpDir, "diff.diff", `+++ b/pkg/readme.md
@@ -2,0 +3,1 @@
+some doc
`)

		coverPercent, uncovered, err := RunDiffCoverage(filepath.Join(tmpDir, "cover.out"), filepath.Join(tmpDir, "diff.diff"), tmpDir, 0.0)
		if err != nil {
			t.Fatalf("Did NOT expect an error here, got: %v", err)
		}
		if coverPercent != 100.0 {
			t.Errorf("Expected 100%% coverage, got %.2f%%", coverPercent)
		}
		if uncovered != nil {
			t.Errorf("Expected uncovered == nil, got %#v", uncovered)
		}
	})

	t.Run("totalNewLines == 0 => returns 100% with uncovered map", func(t *testing.T) {
		// Lines in the diff are outside any function body => totalNewLines=0 => coverage=100
		tmpDir := t.TempDir()
		writeGoMod(t, tmpDir, "github.com/example/module")

		// A cover file that covers line 3 in pkg/foo.go
		writeCoverFile(t, tmpDir, "cover.out", `mode: set
github.com/example/module/pkg/foo.go:3.0,3.10 1 1
`)

		// We'll create a real .go file with a function that spans lines 3..5
		mustWriteFile(t, filepath.Join(tmpDir, "pkg", "foo.go"), `
package foo

func Foo() {
	// lines 3..5
}
`)

		// Our diff references line 10 => outside function => totalNewLines=0
		writeDiffFile(t, tmpDir, "diff.diff", `+++ b/pkg/foo.go
@@ -9,0 +10,1 @@
+some new line
`)

		coverPercent, uncovered, err := RunDiffCoverage(filepath.Join(tmpDir, "cover.out"), filepath.Join(tmpDir, "diff.diff"), tmpDir, 0.0)
		if err != nil {
			t.Fatalf("Did NOT expect an error, got %v", err)
		}
		if coverPercent != 100.0 {
			t.Errorf("Expected 100%% coverage, got %.2f%%", coverPercent)
		}
		// If totalNewLines=0, we return 100.0 coverage and possibly an empty uncovered map.
		if len(uncovered) == 0 {
			t.Logf("uncovered is empty, as expected, since line 10 is outside any function.")
		} else {
			t.Errorf("Expected uncovered to be empty, got: %#v", uncovered)
		}
	})

	t.Run("coverage < minCoverage => error", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeGoMod(t, tmpDir, "github.com/example/module")

		// coverage=0 for lines 2..4
		writeCoverFile(t, tmpDir, "cover.out", `mode: set
github.com/example/module/pkg/foo.go:2.0,4.10 1 0
`)

		// .go file lines 2..4 => function
		mustWriteFile(t, filepath.Join(tmpDir, "pkg", "foo.go"), `package foo
func Foo() {
	// lines 2..4
}
`)

		// The diff references line 3 => inside the function range => coverage=0 => < min => error
		writeDiffFile(t, tmpDir, "diff.diff", `+++ b/pkg/foo.go
@@ -1,0 +3,1 @@
+// line 3
`)

		coveragePercent, uncovered, err := RunDiffCoverage(filepath.Join(tmpDir, "cover.out"), filepath.Join(tmpDir, "diff.diff"), tmpDir, 50.0)
		if err == nil {
			t.Fatalf("Expected coverage < minCoverage error, got nil")
		}
		if coveragePercent != 0 {
			t.Errorf("Expected coverage=0, got %.2f", coveragePercent)
		}
		if len(uncovered) != 1 {
			t.Errorf("Expected exactly 1 uncovered line, got %d", len(uncovered))
		}
	})

	t.Run("coverage >= minCoverage => success", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeGoMod(t, tmpDir, "github.com/example/module")

		// coverage=1 => fully covered lines
		writeCoverFile(t, tmpDir, "cover.out", `mode: set
github.com/example/module/pkg/foo.go:3.0,3.10 1 1
`)

		// Real function from line 3..5
		mustWriteFile(t, filepath.Join(tmpDir, "pkg", "foo.go"), `
package foo

func Foo() {
	// lines 3..5
}
`)

		// Diff references line 3 => inside function => covered => coverage = 100%
		writeDiffFile(t, tmpDir, "diff.diff", `+++ b/pkg/foo.go
@@ -2,0 +3,1 @@
+// line 3
`)

		coverPercent, uncovered, err := RunDiffCoverage(filepath.Join(tmpDir, "cover.out"), filepath.Join(tmpDir, "diff.diff"), tmpDir, 50.0)
		if err != nil {
			t.Fatalf("Expected success, got error: %v", err)
		}
		if coverPercent != 100.0 {
			t.Errorf("Expected 100%% coverage, got %.2f", coverPercent)
		}
		if len(uncovered) != 0 {
			t.Errorf("Expected no uncovered lines, got %#v", uncovered)
		}
	})

}

// ---------------------------------------------------------------
// Helper functions to keep test code DRY
// ---------------------------------------------------------------

func writeGoMod(t *testing.T, dir, moduleName string) {
	t.Helper()
	content := fmt.Sprintf("module %s\n\ngo 1.18\n", moduleName)
	mustWriteFile(t, filepath.Join(dir, "go.mod"), content)
}

func writeCoverFile(t *testing.T, dir, name, content string) {
	t.Helper()
	mustWriteFile(t, filepath.Join(dir, name), content)
}

func writeDiffFile(t *testing.T, dir, name, content string) {
	t.Helper()
	mustWriteFile(t, filepath.Join(dir, name), content)
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("Failed to create directories for %s: %v", path, err)
	}
	if err := ioutil.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file %s: %v", path, err)
	}
}
