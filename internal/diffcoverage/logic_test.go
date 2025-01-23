package diffcoverage

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestParseGoMod_Success tests the happy path for parseGoMod.
func TestParseGoMod_Success(t *testing.T) {
	tmpDir := t.TempDir()
	goModContent := `module github.com/example/module

go 1.18
`
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}

	moduleName, err := parseGoMod(goModPath)
	if err != nil {
		t.Fatalf("parseGoMod failed unexpectedly: %v", err)
	}
	if want := "github.com/example/module"; moduleName != want {
		t.Errorf("parseGoMod returned %q, want %q", moduleName, want)
	}
}

// TestParseGoMod_FileOpenError tests error when go.mod can't be opened.
func TestParseGoMod_FileOpenError(t *testing.T) {
	nonExistentPath := "/path/that/does/not/exist/go.mod"
	_, err := parseGoMod(nonExistentPath)
	if err == nil {
		t.Fatalf("Expected an error for non-existent path, got nil")
	}
	if !strings.Contains(err.Error(), "failed to open go.mod") {
		t.Errorf("Expected error message to contain 'failed to open go.mod', got %v", err)
	}
}

// TestParseGoMod_NoModuleLine tests parseGoMod with a go.mod file missing a `module` line.
func TestParseGoMod_NoModuleLine(t *testing.T) {
	tmpDir := t.TempDir()
	goModContent := `go 1.18`
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}

	_, err := parseGoMod(goModPath)
	if err == nil {
		t.Fatalf("Expected error when no module line is present in go.mod")
	}
	if !strings.Contains(err.Error(), "module name not found") {
		t.Errorf("Expected error to mention 'module name not found', got %v", err)
	}
}

// TestParseCoverFile_Basic tests a normal cover file with valid lines.
func TestParseCoverFile_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	coverFileContent := `mode: set
github.com/example/module/pkg/foo.go:10.0,12.0 2 1
github.com/example/module/pkg/foo.go:15.0,15.10 1 0
github.com/example/module/internal/bar.go:5.0,6.0 1 2
`
	coverFilePath := filepath.Join(tmpDir, "cover.out")
	if err := os.WriteFile(coverFilePath, []byte(coverFileContent), 0644); err != nil {
		t.Fatalf("Failed to write cover.out: %v", err)
	}

	moduleName := "github.com/example/module"
	cd, err := parseCoverFile(coverFilePath, moduleName)
	if err != nil {
		t.Fatalf("parseCoverFile failed unexpectedly: %v", err)
	}

	// foo.go -> lines 10,11,12 covered
	if !cd.CoveredLines["pkg/foo.go"][10] ||
		!cd.CoveredLines["pkg/foo.go"][11] ||
		!cd.CoveredLines["pkg/foo.go"][12] {
		t.Errorf("pkg/foo.go lines 10..12 should be covered")
	}
	// line 15 not covered
	if cd.CoveredLines["pkg/foo.go"][15] {
		t.Errorf("pkg/foo.go line 15 should NOT be covered")
	}

	// internal/bar.go -> lines 5..6 covered
	if !cd.CoveredLines["internal/bar.go"][5] || !cd.CoveredLines["internal/bar.go"][6] {
		t.Errorf("internal/bar.go lines 5,6 should be covered")
	}
}

// TestParseCoverFile_FileOpenError tests when cover file cannot be opened.
func TestParseCoverFile_FileOpenError(t *testing.T) {
	moduleName := "github.com/example/module"
	nonExistentCover := "/path/that/does/not/exist/cover.out"

	_, err := parseCoverFile(nonExistentCover, moduleName)
	if err == nil {
		t.Fatalf("Expected an error, got nil")
	}
}

// TestParseCoverFile_InvalidLines tests various malformed lines that should trigger 'continue'.
func TestParseCoverFile_InvalidLines(t *testing.T) {
	tmpDir := t.TempDir()
	coverFilePath := filepath.Join(tmpDir, "cover_invalid.out")
	moduleName := "github.com/example/module"

	// Each line is crafted to exercise a different 'continue' branch in parseCoverFile.
	coverContent := `mode: set
# 1) Not enough parts
github.com/example/module/pkg/foo.go:23.21,28.2 3
# 2) coverageCount not an integer
github.com/example/module/pkg/foo.go:23.21,28.2 1 notAnInt
# 3) rangeSplit not 2
github.com/example/module/pkg/foo.go:23.21,28.2,extra 1 1
# 4) start/endSplit not 2
github.com/example/module/pkg/foo.go:23.21.???,28.2 1 1
# 5) startLine parse error
github.com/example/module/pkg/foo.go:abc.21,30.2 1 1
# 6) endLine parse error
github.com/example/module/pkg/foo.go:23.21,NaN.2 1 1

# Finally, a valid line that should be parsed
github.com/example/module/pkg/foo.go:10.0,12.0 2 1
`
	if err := os.WriteFile(coverFilePath, []byte(coverContent), 0644); err != nil {
		t.Fatalf("Failed to write invalid cover file: %v", err)
	}

	coverage, err := parseCoverFile(coverFilePath, moduleName)
	if err != nil {
		t.Fatalf("parseCoverFile returned unexpected error: %v", err)
	}

	// All invalid lines should be skipped; only the valid line remains:
	// => lines 10..12 in pkg/foo.go are covered
	covMap := coverage.CoveredLines["pkg/foo.go"]
	if covMap == nil {
		t.Fatalf("Expected 'pkg/foo.go' to be present due to valid line, but not found.")
	}
	for ln := 10; ln <= 12; ln++ {
		if !covMap[ln] {
			t.Errorf("Expected line %d to be covered, but it's missing.", ln)
		}
	}
}

// TestParseDiffFile_Simple ensures parseDiffFile logic for new lines.
func TestParseDiffFile_Simple(t *testing.T) {
	tmpDir := t.TempDir()
	diffFileContent := `+++ b/pkg/foo.go
@@ -10,0 +10,2 @@
+line1
+line2
+++ b/pkg/foo_test.go
@@ -5,0 +5,1 @@
+line_test
+++ b/internal/bar.go
@@ -7,0 +7,2 @@
+barline1
+barline2
`
	diffFilePath := filepath.Join(tmpDir, "diff.txt")
	if err := os.WriteFile(diffFilePath, []byte(diffFileContent), 0644); err != nil {
		t.Fatalf("Failed to write diff.txt: %v", err)
	}

	moduleName := "github.com/example/module"
	dd, err := parseDiffFile(diffFilePath, moduleName)
	if err != nil {
		t.Fatalf("parseDiffFile failed: %v", err)
	}

	// Skip foo_test.go => only foo.go + internal/bar.go
	if len(dd.NewLines) != 2 {
		t.Fatalf("Expected 2 files, got %d", len(dd.NewLines))
	}

	fooLines := dd.NewLines["github.com/example/module/pkg/foo.go"]
	if !fooLines[10] || !fooLines[11] {
		t.Errorf("Expected lines 10,11 for foo.go")
	}
	barLines := dd.NewLines["github.com/example/module/internal/bar.go"]
	if !barLines[7] || !barLines[8] {
		t.Errorf("Expected lines 7,8 for bar.go")
	}
}

// TestParseGoFiles_Basic checks that function lines are extracted.
func TestParseGoFiles_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	src := `package testpkg

func Foo() {
	// line 4
}

func Bar() {
	// line 9
}
`
	goFilePath := filepath.Join(tmpDir, "file.go")
	if err := os.WriteFile(goFilePath, []byte(src), 0644); err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	funcLines, err := parseGoFiles(tmpDir, []string{"file.go"})
	if err != nil {
		t.Fatalf("parseGoFiles returned unexpected error: %v", err)
	}
	ranges := funcLines.Functions["file.go"]
	if len(ranges) != 2 {
		t.Fatalf("Expected 2 function ranges, got %d", len(ranges))
	}
	for i, r := range ranges {
		if r[0] >= r[1] {
			t.Errorf("Function %d has invalid range: %v", i, r)
		}
	}
}

// TestIsLineInFunctions checks boundary cases of isLineInFunctions.
func TestIsLineInFunctions(t *testing.T) {
	fl := &FuncLines{
		Functions: map[string][][2]int{
			"file.go": {
				{3, 5},
				{7, 9},
			},
		},
	}

	cases := []struct {
		file string
		line int
		want bool
	}{
		{"file.go", 2, false},
		{"file.go", 3, true},
		{"file.go", 5, true},
		{"file.go", 6, false},
		{"file.go", 7, true},
		{"file.go", 9, true},
		{"file.go", 10, false},
		{"other.go", 3, false},
	}
	for _, c := range cases {
		got := isLineInFunctions(c.file, c.line, fl)
		if got != c.want {
			t.Errorf("isLineInFunctions(%q,%d) = %v, want %v", c.file, c.line, got, c.want)
		}
	}
}

// TestGroupLinesIntoRanges ensures grouping logic works for empty & normal cases.
func TestGroupLinesIntoRanges(t *testing.T) {
	tests := []struct {
		name     string
		input    []int
		expected [][2]int
	}{
		{"empty", []int{}, nil},
		{"single_line", []int{10}, [][2]int{{10, 10}}},
		{"contiguous", []int{1, 2, 3}, [][2]int{{1, 3}}},
		{"disjoint", []int{1, 2, 4, 5, 6}, [][2]int{{1, 2}, {4, 6}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GroupLinesIntoRanges(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("groupLinesIntoRanges(%v) got %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseGoFiles_AllBranches(t *testing.T) {
	tmpDir := t.TempDir()

	// 1) Non-existent file:
	// We'll just use a file name that doesn't exist, e.g. "not_found.go".
	// parseGoFiles will try os.Stat(...) and fail, triggering continue.
	nonExistent := "not_found.go"

	// 2) Directory: We'll create a directory named "someDir"
	dirName := "someDir"
	dirPath := filepath.Join(tmpDir, dirName)
	if err := os.Mkdir(dirPath, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// 3) File without .go suffix: "file.txt"
	txtFileName := "file.txt"
	txtFilePath := filepath.Join(tmpDir, txtFileName)
	if err := os.WriteFile(txtFilePath, []byte("just some text"), 0644); err != nil {
		t.Fatalf("Failed to write text file: %v", err)
	}

	// 4) Invalid .go file that triggers parser.ParseFile error:
	// We'll put a syntax error (like 'package 123' or incomplete code).
	invalidGoName := "invalid.go"
	invalidGoPath := filepath.Join(tmpDir, invalidGoName)
	invalidContent := `package ???  // invalid syntax`
	if err := os.WriteFile(invalidGoPath, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("Failed to write invalid .go file: %v", err)
	}

	// 5) Valid .go file that successfully parses:
	// We'll include a simple function to confirm that the function range is recorded.
	validGoName := "valid.go"
	validGoPath := filepath.Join(tmpDir, validGoName)
	validContent := `package example

func Foo() {
	// line 3
}
`
	if err := os.WriteFile(validGoPath, []byte(validContent), 0644); err != nil {
		t.Fatalf("Failed to write valid .go file: %v", err)
	}

	// We'll call parseGoFiles with a slice containing all the above files/dirs.
	files := []string{
		nonExistent,
		dirName,
		txtFileName,
		invalidGoName,
		validGoName,
	}

	funcLines, err := parseGoFiles(tmpDir, files)
	if err != nil {
		t.Fatalf("parseGoFiles returned unexpected error: %v", err)
	}

	// We expect only "valid.go" to be parsed successfully.
	// Key in the map should be "valid.go" (normalized path).
	lines, ok := funcLines.Functions["valid.go"]
	if !ok {
		t.Fatalf("Expected 'valid.go' to appear in funcLines, but wasn't found.")
	}

	// Check that we actually got a function range for Foo().
	// If everything is correct, there should be exactly 1 range.
	if len(lines) != 1 {
		t.Fatalf("Expected 1 function range in valid.go, got %d", len(lines))
	}

	r := lines[0]
	if r[0] >= r[1] {
		t.Errorf("Expected a valid range (start < end), got start=%d, end=%d", r[0], r[1])
	}

	// Confirm that none of the other entries were added, e.g. "invalid.go" shouldn't appear.
	if len(funcLines.Functions) != 1 {
		t.Errorf("Expected only valid.go in funcLines, but found %d entries: %v",
			len(funcLines.Functions), funcLines.Functions)
	}
}

// TestParseDiffFile_FileOpenError covers the "os.Open(diffFilePath)" error case.
func TestParseDiffFile_FileOpenError(t *testing.T) {
	moduleName := "github.com/example/module"
	nonExistentPath := "/this/path/does/not/exist.diff"

	_, err := parseDiffFile(nonExistentPath, moduleName)
	if err == nil {
		t.Fatalf("Expected error for non-existent diff file, but got nil")
	}
}

// TestParseDiffFile_AllBranches ensures 100% coverage of parseDiffFile.
func TestParseDiffFile_AllBranches(t *testing.T) {
	tmpDir := t.TempDir()
	moduleName := "github.com/example/module"

	// We'll build a diff file that triggers all the branches:
	// 1) "+++ " lines with missing fields
	// 2) "+++ b/pkg/foo.go" -> normal usage
	// 3) "+++ b/pkg/foo_test.go" -> skip test file
	// 4) "+++ b/pkg/notgo.txt" -> skip non-go file
	// 5) hunk headers matching @@ -start,len +start,len @@
	// 6) lines starting with '+' but currentFile == "" -> skip
	// 7) lines starting with '+' for the valid .go file
	// 8) lines starting with '+' for the test.go, which we skip

	diffContent := strings.Join([]string{
		// 1) "+++ " line but missing fields (simulate incomplete line)
		"+++ ",

		// 2) Proper "+++ b/pkg/foo.go"
		"+++ b/pkg/foo.go",

		// hunk header with start=10
		"@@ -9,0 +10,3 @@",

		// 6) '+' line while currentFile != "" but let's also test
		// a scenario "currentFile == ''" by putting a plus BEFORE a '+++ ' line:
		// We'll do that above, so let's do a plus line first:
		// This plus line occurs after currentFile is set, so it should be used
		"+someAddedLine", // plusStartLine=10 => line 10

		"+anotherLine", // line 11
		"+andMore",     // line 12

		// 3) test file: "+++ b/pkg/foo_test.go"
		"+++ b/pkg/foo_test.go",
		"@@ -5,0 +6,1 @@",
		"+line_test", // should be skipped due to _test.go

		// 4) non-.go file: "+++ b/pkg/notgo.txt"
		"+++ b/pkg/notgo.txt",
		"@@ -10,0 +10,2 @@",
		"+someLineInTxt", // should be skipped because not .go
		"+anotherLineInTxt",

		// 5) lines starting with '+' but currentFile == "" (we do that at the start)
		// We'll place them at the end or top to ensure coverage
	}, "\n")

	// Insert a plus line before we set currentFile to trigger skip
	diffContent = "+thisLineShouldBeSkippedBecauseNoFileSet\n" + diffContent

	diffFilePath := filepath.Join(tmpDir, "sample.diff")
	if err := os.WriteFile(diffFilePath, []byte(diffContent), 0644); err != nil {
		t.Fatalf("Failed to write diff file: %v", err)
	}

	// Call parseDiffFile
	dd, err := parseDiffFile(diffFilePath, moduleName)
	if err != nil {
		t.Fatalf("parseDiffFile returned unexpected error: %v", err)
	}

	// Now let's check we got the lines for the valid .go file

	// We expect "github.com/example/module/pkg/foo.go" to have lines 10, 11, 12
	wantFile := filepath.Join(moduleName, "pkg", "foo.go")
	wantFile = filepath.ToSlash(wantFile)

	// We expect exactly 1 entry in dd.NewLines for the valid .go file
	// Check how many files we actually got
	if len(dd.NewLines) != 1 {
		t.Fatalf("Expected 1 file in dd.NewLines, got %d: %v", len(dd.NewLines), dd.NewLines)
	}

	linesMap, ok := dd.NewLines[wantFile]
	if !ok {
		t.Fatalf("Missing expected file %q in dd.NewLines; found keys: %v",
			wantFile, reflect.ValueOf(dd.NewLines).MapKeys())
	}

	// Expect lines 10, 11, 12
	for _, ln := range []int{10, 11, 12} {
		if !linesMap[ln] {
			t.Errorf("Expected line %d to be set in dd.NewLines[%q], but it wasn't",
				ln, wantFile)
		}
	}

	// Ensure we DID NOT parse foo_test.go or notgo.txt
	for key := range dd.NewLines {
		if strings.Contains(key, "_test.go") {
			t.Errorf("Should not have included test file in dd.NewLines, but found %q", key)
		}
		if strings.HasSuffix(key, ".txt") {
			t.Errorf("Should not have included .txt file in dd.NewLines, but found %q", key)
		}
	}
}

func TestParseCoverFile_AllBranches(t *testing.T) {
	tmpDir := t.TempDir()
	moduleName := "github.com/example/module"

	// We'll build a cover.out file that has:
	// 1) A "mode:" line (should be skipped)
	// 2) A line with fewer than 3 parts
	// 3) A line where pathAndRange doesn't split into 2
	// 4) A line where absPath doesn't start with moduleName+"/"
	// 5) A line where coverageCount is not an integer
	// 6) A line where rangeSplit != 2
	// 7) A line where startSplit or endSplit != 2
	// 8) A line where startLine parse fails
	// 9) A line where endLine parse fails
	// 10) A valid line that actually sets coverage

	coverContent := strings.Join([]string{
		// 1) "mode:" line
		"mode: atomic",

		// 2) Not enough parts (only 2 parts instead of 3)
		"github.com/example/module/pkg/foo.go:10.10,12.10 2",

		// 3) pathAndRange doesn't split into 2
		"github.com/example/module/pkg/foo.go 1 1",

		// 4) absPath not starting with moduleName + "/"
		"github.com/other/module/pkg/foo.go:10.10,12.10 2 1",

		// 5) coverageCount not integer
		"github.com/example/module/pkg/foo.go:10.10,12.10 2 notANumber",

		// 6) rangeSplit != 2
		"github.com/example/module/pkg/foo.go:10.10,12.10,extra 2 1",

		// 7) startSplit or endSplit != 2
		"github.com/example/module/pkg/foo.go:10.10.??? ,12.10 2 1",

		// 8) startLine parse fails
		"github.com/example/module/pkg/foo.go:abc.0,15.0 2 1",

		// 9) endLine parse fails
		"github.com/example/module/pkg/foo.go:10.0,NaN.0 2 1",

		// 10) Valid line that sets coverage (lines 10..12)
		"github.com/example/module/pkg/foo.go:10.0,12.0 2 1",
	}, "\n")

	coverFilePath := filepath.Join(tmpDir, "cover.out")
	if err := os.WriteFile(coverFilePath, []byte(coverContent), 0644); err != nil {
		t.Fatalf("Failed to write cover file: %v", err)
	}

	coverage, err := parseCoverFile(coverFilePath, moduleName)
	if err != nil {
		t.Fatalf("parseCoverFile returned unexpected error: %v", err)
	}

	// We expect only the valid line (#10) to produce coverage
	// That line covers lines 10,11,12 in "pkg/foo.go"

	if len(coverage.CoveredLines) == 0 {
		t.Fatalf("Expected at least one file in CoveredLines due to valid line.")
	}
	covMap := coverage.CoveredLines["pkg/foo.go"]
	if covMap == nil {
		t.Fatalf("Expected 'pkg/foo.go' coverage entry, but not found.")
	}

	for ln := 10; ln <= 12; ln++ {
		if !covMap[ln] {
			t.Errorf("Expected line %d to be covered, but it's missing.", ln)
		}
	}
}

// TestParseCoverFile_OpenError covers the file-open error path.
func TestParseCoverFile_OpenError(t *testing.T) {
	nonExistentPath := "/definitely/does/not/exist/cover.out"
	_, err := parseCoverFile(nonExistentPath, "github.com/example/module")
	if err == nil {
		t.Fatalf("Expected an error for non-existent file, got nil")
	}
}
