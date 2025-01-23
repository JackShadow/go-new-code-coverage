package diffcoverage

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// CoverageData holds coverage information: for each file, a set of covered lines.
type CoverageData struct {
	CoveredLines map[string]map[int]bool // file -> set of covered lines
}

// DiffData holds information about new/changed lines from the diff.
type DiffData struct {
	NewLines map[string]map[int]bool // file -> set of new/changed lines
}

// FuncLines holds ranges of function lines for each file.
type FuncLines struct {
	Functions map[string][][2]int // file -> slice of [start, end] function lines
}

// parseGoMod reads the go.mod file and returns the module name.
func parseGoMod(goModPath string) (string, error) {
	file, err := os.Open(goModPath)
	if err != nil {
		return "", fmt.Errorf("failed to open go.mod: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return fields[1], nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading go.mod: %v", err)
	}

	return "", fmt.Errorf("module name not found in go.mod")
}

// parseCoverFile parses the cover.out file and returns CoverageData.
func parseCoverFile(coverFilePath, moduleName string) (*CoverageData, error) {
	f, err := os.Open(coverFilePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	coverage := &CoverageData{
		CoveredLines: make(map[string]map[int]bool),
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// Skip the line starting with "mode:"
		if strings.HasPrefix(line, "mode:") {
			continue
		}

		// Format: filepath.go:startLine.startCol,endLine.endCol numStatements count
		parts := strings.Split(line, " ")
		if len(parts) != 3 {
			continue
		}
		fileRange := parts[0]
		_, coverageCountStr := parts[1], parts[2]

		pathAndRange := strings.Split(fileRange, ":")
		if len(pathAndRange) != 2 {
			continue
		}
		absPath := pathAndRange[0]
		rangePart := pathAndRange[1]

		// Check if path starts with the module name
		if !strings.HasPrefix(absPath, moduleName+"/") {
			continue
		}
		relPath := strings.TrimPrefix(absPath, moduleName+"/")

		coverageCount, err := strconv.Atoi(coverageCountStr)
		if err != nil {
			continue
		}

		rangeSplit := strings.Split(rangePart, ",")
		if len(rangeSplit) != 2 {
			continue
		}
		startSplit := strings.Split(rangeSplit[0], ".")
		endSplit := strings.Split(rangeSplit[1], ".")

		if len(startSplit) != 2 || len(endSplit) != 2 {
			continue
		}

		startLine, err := strconv.Atoi(startSplit[0])
		if err != nil {
			continue
		}
		endLine, err := strconv.Atoi(endSplit[0])
		if err != nil {
			continue
		}

		// If coverageCount > 0, mark ALL lines in the range as covered
		if coverageCount > 0 {
			normalizedPath := filepath.ToSlash(relPath)
			if coverage.CoveredLines[normalizedPath] == nil {
				coverage.CoveredLines[normalizedPath] = make(map[int]bool)
			}
			for ln := startLine; ln <= endLine; ln++ {
				coverage.CoveredLines[normalizedPath][ln] = true
			}
		}
	}

	return coverage, scanner.Err()
}

// parseDiffFile parses the diff with --unified=0 and returns DiffData with new/changed lines.
func parseDiffFile(diffFilePath, moduleName string) (*DiffData, error) {
	f, err := os.Open(diffFilePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	diffData := &DiffData{
		NewLines: make(map[string]map[int]bool),
	}

	// Regex for @@ -start,len +start,len @@
	hunkHeaderRegex := regexp.MustCompile(`@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

	var currentFile string
	var plusStartLine int

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		// Example: "+++ b/pkg/foo.go"
		if strings.HasPrefix(line, "+++ ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				path := fields[1] // e.g. b/pkg/foo.go
				path = strings.TrimPrefix(path, "b/")
				// Prepend the module name
				fullPath := filepath.Join(moduleName, path)
				normalizedPath := filepath.ToSlash(fullPath)
				currentFile = normalizedPath
			}
			continue
		}

		// Look for hunk headers
		if hunkHeaderRegex.MatchString(line) {
			matches := hunkHeaderRegex.FindStringSubmatch(line)
			if len(matches) >= 3 {
				newStart, _ := strconv.Atoi(matches[2])
				plusStartLine = newStart
			}
			continue
		}

		// If line starts with '+', it's an added line
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++ ") {
			if currentFile == "" {
				continue
			}
			// Skip test files
			if strings.Contains(currentFile, "_test.go") {
				continue
			}
			// Only handle .go files
			if !strings.HasSuffix(currentFile, ".go") {
				continue
			}

			// Ensure the map for the file is initialized
			if diffData.NewLines[currentFile] == nil {
				diffData.NewLines[currentFile] = make(map[int]bool)
			}
			diffData.NewLines[currentFile][plusStartLine] = true
			plusStartLine++
		}
	}

	return diffData, scanner.Err()
}

// parseGoFiles parses only the given .go files and extracts the ranges of function lines.
// Excludes the last line of each function from the range.
func parseGoFiles(rootDir string, files []string) (*FuncLines, error) {
	funcLines := &FuncLines{
		Functions: make(map[string][][2]int),
	}

	for _, relPath := range files {
		fullPath := filepath.Join(rootDir, relPath)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}
		if !strings.HasSuffix(fullPath, ".go") {
			continue
		}

		fset := token.NewFileSet()
		astFile, err := parser.ParseFile(fset, fullPath, nil, 0)
		if err != nil {
			continue
		}

		normalizedPath := filepath.ToSlash(relPath)
		for _, decl := range astFile.Decls {
			if funcDecl, ok := decl.(*ast.FuncDecl); ok {
				start := fset.Position(funcDecl.Pos()).Line
				end := fset.Position(funcDecl.End()).Line
				// Exclude the last line
				if end > start {
					end--
				}
				funcLines.Functions[normalizedPath] = append(funcLines.Functions[normalizedPath], [2]int{start, end})
			}
		}
	}

	return funcLines, nil
}

// isLineInFunctions checks if the given line is within any function range in the file.
func isLineInFunctions(file string, line int, funcLines *FuncLines) bool {
	ranges, exists := funcLines.Functions[file]
	if !exists {
		return false
	}
	for _, r := range ranges {
		if line >= r[0] && line <= r[1] {
			return true
		}
	}
	return false
}

// GroupLinesIntoRanges groups a sorted list of lines into contiguous ranges.
func GroupLinesIntoRanges(lines []int) [][2]int {
	if len(lines) == 0 {
		return nil
	}

	var ranges [][2]int
	start := lines[0]
	end := lines[0]

	for _, line := range lines[1:] {
		if line == end+1 {
			end = line
		} else {
			ranges = append(ranges, [2]int{start, end})
			start = line
			end = line
		}
	}
	ranges = append(ranges, [2]int{start, end})
	return ranges
}
