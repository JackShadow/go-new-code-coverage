package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// CoverageData хранит информацию по покрытию: для каждого файла — множество покрытых строк.
type CoverageData struct {
	CoveredLines map[string]map[int]bool // file -> set of covered lines
}

// DiffData хранит информацию о новых/изменённых строках из diff.
type DiffData struct {
	NewLines map[string]map[int]bool // file -> set of new/changed lines
}

// FuncLines хранит диапазоны строк функций для каждого файла.
type FuncLines struct {
	Functions map[string][][2]int // file -> slice из [start, end] строк функций
}

// parseGoMod читает файл go.mod и возвращает название модуля.
func parseGoMod(goModPath string) (string, error) {
	file, err := os.Open(goModPath)
	if err != nil {
		return "", fmt.Errorf("не удалось открыть go.mod: %v", err)
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
		return "", fmt.Errorf("ошибка чтения go.mod: %v", err)
	}

	return "", fmt.Errorf("название модуля не найдено в go.mod")
}

// CoverageData разбирает файл покрытия и возвращает CoverageData.
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
		// Пропускаем строку с "mode: set"
		if strings.HasPrefix(line, "mode:") {
			continue
		}

		// Формат: filepath.go:startLine.startCol,endLine.endCol numStatements count
		parts := strings.Split(line, " ")
		if len(parts) != 3 {
			continue
		}
		fileRange := parts[0] // filepath.go:23.21,28.2
		_, coverageCountStr := parts[1], parts[2]

		// Отделим путь от диапазона
		pathAndRange := strings.Split(fileRange, ":")
		if len(pathAndRange) != 2 {
			continue
		}
		absPath := pathAndRange[0]   // "github.com/user/repo/pkg/foo.go"
		rangePart := pathAndRange[1] // "23.21,28.2"

		// Проверяем, начинается ли путь с названия модуля
		if !strings.HasPrefix(absPath, moduleName+"/") {
			// Возможно, путь абсолютный без названия модуля
			// Можно попробовать использовать относительный путь
			continue
		}
		relPath := strings.TrimPrefix(absPath, moduleName+"/") // "pkg/foo.go"

		coverageCount, err := strconv.Atoi(coverageCountStr)
		if err != nil {
			continue
		}

		// Разберём "23.21,28.2" -> startLine = 23, endLine = 28
		rangeSplit := strings.Split(rangePart, ",")
		if len(rangeSplit) != 2 {
			continue
		}
		start := strings.Split(rangeSplit[0], ".")
		end := strings.Split(rangeSplit[1], ".")

		if len(start) != 2 || len(end) != 2 {
			continue
		}

		startLine, err := strconv.Atoi(start[0])
		if err != nil {
			continue
		}
		endLine, err := strconv.Atoi(end[0])
		if err != nil {
			continue
		}

		// Считаем, что если coverageCount > 0, то ВСЕ строки в диапазоне покрыты
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

// parseDiffFile разбирает diff с --unified=0 и формирует список новых/изменённых строк.
func parseDiffFile(diffFilePath, moduleName string) (*DiffData, error) {
	f, err := os.Open(diffFilePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	diffData := &DiffData{
		NewLines: make(map[string]map[int]bool),
	}

	// Регек для @@ -start,len +start,len @@
	// Пример попадания: @@ -10,0 +11,3 @@
	hunkHeaderRegex := regexp.MustCompile(`@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

	var currentFile string
	var plusStartLine int // из +xx

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		// Пример: "diff --git a/pkg/foo.go b/pkg/foo.go"
		// Ищем "+++ b/..." — там указывается новый путь
		if strings.HasPrefix(line, "+++ ") {
			// Пример: +++ b/pkg/foo.go
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				path := fields[1] // b/pkg/foo.go
				// убираем префикс "b/"
				path = strings.TrimPrefix(path, "b/")
				// Добавляем префикс модуля
				fullPath := filepath.Join(moduleName, path)
				normalizedPath := filepath.ToSlash(fullPath)
				currentFile = normalizedPath
			}
			continue
		}

		// Ищем заголовки хунка
		if hunkHeaderRegex.MatchString(line) {
			matches := hunkHeaderRegex.FindStringSubmatch(line)
			// matches[1] - oldStart
			// matches[2] - newStart
			newStart, _ := strconv.Atoi(matches[2])
			plusStartLine = newStart
			continue
		}

		// Если строка начинается с '+', значит это добавленная строка
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++ ") {
			// Учитываем, что мы в контексте currentFile
			if currentFile == "" {
				continue
			}
			// skip test
			if strings.Contains(currentFile, "_test.go") || strings.Contains(currentFile, "coverage") {
				continue
			}
			// file не go
			if !strings.HasSuffix(currentFile, ".go") {
				continue
			}

			diffData.NewLines = ensureMap(diffData.NewLines, currentFile)
			// Поскольку unified=0, каждая +строка фактически занимает одну строку в новом файле.
			// plusStartLine указывает, с какой строчки начинается текущий хунк.
			// После каждой +строки повышаем счётчик на 1.
			diffData.NewLines[currentFile][plusStartLine] = true
			plusStartLine++
		}
	}

	return diffData, scanner.Err()
}

// ensureMap инициализирует карту для файла, если она не инициализирована.
func ensureMap(m map[string]map[int]bool, file string) map[string]map[int]bool {
	if m[file] == nil {
		m[file] = make(map[int]bool)
	}
	return m
}

// parseGoFiles парсит только указанные Go-файлы и извлекает диапазоны строк функций.
// Исключает последнюю строку функции из диапазона.
func parseGoFiles(rootDir string, files []string) (*FuncLines, error) {
	funcLines := &FuncLines{
		Functions: make(map[string][][2]int),
	}

	for _, relPath := range files {
		// Формируем полный путь к файлу
		fullPath := filepath.Join(rootDir, relPath)
		info, err := os.Stat(fullPath)
		if err != nil {
			fmt.Printf("Cannot stat file %s: %v\n", fullPath, err)
			continue // Пропускаем отсутствующие файлы
		}

		// Пропускаем директории, хотя в списке файлов должны быть только файлы
		if info.IsDir() {
			continue
		}

		// Обрабатываем только .go файлы
		if !strings.HasSuffix(fullPath, ".go") {
			continue
		}

		// Открываем файл и парсим его
		fset := token.NewFileSet()
		astFile, err := parser.ParseFile(fset, fullPath, nil, 0)
		if err != nil {
			fmt.Printf("Error parsing file %s: %v\n", fullPath, err)
			continue // Пропускаем файл с ошибкой
		}

		normalizedPath := filepath.ToSlash(relPath)

		for _, decl := range astFile.Decls {
			if funcDecl, ok := decl.(*ast.FuncDecl); ok {
				start := fset.Position(funcDecl.Pos()).Line
				end := fset.Position(funcDecl.End()).Line

				// Исключаем последнюю строку функции
				if end > start {
					end = end - 1
				}

				funcLines.Functions[normalizedPath] = append(funcLines.Functions[normalizedPath], [2]int{start, end})
			}
		}
	}

	return funcLines, nil
}

// isLineInFunctions проверяет, находится ли строка в диапазоне функций.
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

// groupLinesIntoRanges группирует отсортированные строки в диапазоны.
func groupLinesIntoRanges(lines []int) [][2]int {
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

	// Добавляем последний диапазон
	ranges = append(ranges, [2]int{start, end})

	return ranges
}

func main() {
	// Определение флагов
	verbose := flag.Bool("vvv", false, "Verbose output: list lines not covered")
	minCoverage := flag.Float64("min", 0.0, "Minimum coverage percentage (e.g., 80.0)")

	// Дополнительные флаги для поддержки длинных форм
	flag.BoolVar(verbose, "verbose", false, "Verbose output: list lines not covered")

	// Парсим флаги
	flag.Parse()

	// Проверка наличия необходимых аргументов
	if flag.NArg() < 3 {
		fmt.Println("Usage: diffcoverage [options] <cover.out> <diff.txt> <source_root>")
		fmt.Println("Options:")
		flag.PrintDefaults()
		os.Exit(1)
	}
	coverPath := flag.Arg(0)
	diffPath := flag.Arg(1)
	sourceRoot := flag.Arg(2)

	// Получаем название модуля из go.mod
	moduleName, err := parseGoMod(sourceRoot + "/go.mod")
	if err != nil {
		fmt.Printf("Error parsing go.mod: %v\n", err)
		os.Exit(1)
	}
	// Вывод для проверки
	// fmt.Printf("Module name: %s\n", moduleName)

	coverageData, err := parseCoverFile(coverPath, moduleName)
	if err != nil {
		fmt.Printf("Error parsing cover file: %v\n", err)
		os.Exit(1)
	}

	diffData, err := parseDiffFile(diffPath, moduleName)
	if err != nil {
		fmt.Printf("Error parsing diff file: %v\n", err)
		os.Exit(1)
	}

	// Получаем список файлов, которые нужно анализировать
	var filesToAnalyze []string
	for file := range diffData.NewLines {
		// Удаляем префикс модуля из пути
		if strings.HasPrefix(file, moduleName+"/") {
			relFile := strings.TrimPrefix(file, moduleName+"/")
			filesToAnalyze = append(filesToAnalyze, relFile)
		} else {
			filesToAnalyze = append(filesToAnalyze, file)
		}
	}

	if len(filesToAnalyze) == 0 {
		fmt.Println("No new/changed Go files found in diff.")
		os.Exit(0)
	}

	funcLines, err := parseGoFiles(sourceRoot, filesToAnalyze)
	if err != nil {
		fmt.Printf("Error parsing Go files: %v\n", err)
		os.Exit(1)
	}

	// Считаем покрытые и все новые строки в функциях
	totalNewLines := 0
	coveredNewLines := 0
	uncoveredLinesMap := make(map[string][]int) // file -> []lines

	for file, newLinesSet := range diffData.NewLines {
		// Удаляем префикс модуля из пути
		var relFile string
		if strings.HasPrefix(file, moduleName+"/") {
			relFile = strings.TrimPrefix(file, moduleName+"/")
		} else {
			relFile = file
		}

		// skip test files
		if strings.Contains(relFile, "_test.go") {
			continue
		}
		if strings.Contains(relFile, "coverage") {
			continue
		}
		// file не go
		if !strings.HasSuffix(relFile, ".go") {
			continue
		}

		for line := range newLinesSet {
			// Проверяем, что строка находится внутри функции
			if !isLineInFunctions(relFile, line, funcLines) {
				continue
			}

			totalNewLines++

			if coverageData.CoveredLines[file] != nil && coverageData.CoveredLines[file][line] {
				coveredNewLines++
			} else {
				// Добавляем непокрытую строку в карту
				uncoveredLinesMap[relFile] = append(uncoveredLinesMap[relFile], line)
			}
		}
	}

	// Сортируем строки в каждой файле
	for file := range uncoveredLinesMap {
		sort.Ints(uncoveredLinesMap[file])
	}

	// Вывод непокрытых строк, если включен режим verbose
	if *verbose && len(uncoveredLinesMap) > 0 {
		fmt.Println("Uncovered lines:")
		for file, lines := range uncoveredLinesMap {
			ranges := groupLinesIntoRanges(lines)
			fmt.Printf("File: %s\n", file)
			fmt.Println("Uncovered lines:")
			for _, r := range ranges {
				if r[0] == r[1] {
					fmt.Printf("- %d\n", r[0])
				} else {
					fmt.Printf("- %d-%d\n", r[0], r[1])
				}
			}
			fmt.Println()
		}
	}

	if totalNewLines == 0 {
		fmt.Println("No new/changed lines in functions found in diff.")
		return
	}

	coveragePercent := 100.0 * float64(coveredNewLines) / float64(totalNewLines)
	fmt.Printf("New/Changed lines coverage in functions: %.2f%% (%d/%d)\n",
		coveragePercent, coveredNewLines, totalNewLines)

	// Проверка минимального покрытия
	if coveragePercent < *minCoverage {
		fmt.Printf("Coverage %.2f%% is below the minimum required %.2f%%\n", coveragePercent, *minCoverage)
		os.Exit(1)
	}
}
