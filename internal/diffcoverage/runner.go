package diffcoverage

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// RunDiffCoverage runs the main diff-coverage logic and returns:
//   - coveragePercent (float64)
//   - uncovered map[file][]lines
//   - error if coverage below minCoverage or parse failures
func RunDiffCoverage(coverPath, diffPath, sourceRoot string, minCoverage float64) (float64, map[string][]int, error) {

	moduleName, err := parseGoMod(filepath.Join(sourceRoot, "go.mod"))
	if err != nil {
		return 0, nil, fmt.Errorf("error parsing go.mod: %v", err)
	}

	coverageData, err := parseCoverFile(coverPath, moduleName)
	if err != nil {
		return 0, nil, fmt.Errorf("error parsing cover file: %v", err)
	}

	diffData, err := parseDiffFile(diffPath, moduleName)
	if err != nil {
		return 0, nil, fmt.Errorf("error parsing diff file: %v", err)
	}

	var filesToAnalyze []string
	for file := range diffData.NewLines {
		if strings.HasPrefix(file, moduleName+"/") {
			relFile := strings.TrimPrefix(file, moduleName+"/")
			filesToAnalyze = append(filesToAnalyze, relFile)
		} else {
			filesToAnalyze = append(filesToAnalyze, file)
		}
	}

	if len(filesToAnalyze) == 0 {
		// No new/changed Go files found
		return 100.0, nil, nil
	}

	funcLines, err := parseGoFiles(sourceRoot, filesToAnalyze)
	if err != nil {
		return 0, nil, fmt.Errorf("error parsing go files: %v", err)
	}

	totalNewLines := 0
	coveredNewLines := 0
	uncoveredLinesMap := make(map[string][]int)

	for file, newLinesSet := range diffData.NewLines {
		var relFile string
		if strings.HasPrefix(file, moduleName+"/") {
			relFile = strings.TrimPrefix(file, moduleName+"/")
		} else {
			relFile = file
		}

		for line := range newLinesSet {
			// Only consider lines inside functions
			if !isLineInFunctions(relFile, line, funcLines) {
				continue
			}
			totalNewLines++
			if coverageData.CoveredLines[relFile] != nil && coverageData.CoveredLines[relFile][line] {
				coveredNewLines++
			} else {
				uncoveredLinesMap[relFile] = append(uncoveredLinesMap[relFile], line)
			}
		}
	}

	// Sort the uncovered lines
	for file := range uncoveredLinesMap {
		sort.Ints(uncoveredLinesMap[file])
	}

	if totalNewLines == 0 {
		// Means we found changed go files, but no lines inside function bodies
		return 100.0, uncoveredLinesMap, nil
	}

	coveragePercent := 100.0 * float64(coveredNewLines) / float64(totalNewLines)

	if coveragePercent < minCoverage {
		return coveragePercent, uncoveredLinesMap,
			fmt.Errorf("coverage %.2f%% is below the minimum required %.2f%%", coveragePercent, minCoverage)
	}

	return coveragePercent, uncoveredLinesMap, nil
}
