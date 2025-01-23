package main

import (
	"flag"
	"fmt"
	"github.com/JackShadow/go-new-code-coverage/internal/diffcoverage"
	"os"
)

func main() {
	verboseFlag := flag.Bool("vvv", false, "Verbose output: list lines not covered")
	minCoverageFlag := flag.Float64("min", 0.0, "Minimum coverage percentage (e.g., 80.0)")
	flag.BoolVar(verboseFlag, "verbose", false, "Verbose output: list lines not covered")

	flag.Parse()

	if flag.NArg() < 3 {
		fmt.Println("Usage: diffcoverage [options] <cover.out> <diff.txt> <source_root>")
		fmt.Println("Options:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	coverPath := flag.Arg(0)
	diffPath := flag.Arg(1)
	sourceRoot := flag.Arg(2)

	coveragePercent, uncovered, err := diffcoverage.RunDiffCoverage(coverPath, diffPath, sourceRoot, *minCoverageFlag)
	if err != nil {
		// Could be coverage below threshold or parse error
		fmt.Println(err.Error())
	}

	// If user wants verbose output, show uncovered lines
	if *verboseFlag && len(uncovered) > 0 {
		fmt.Println("Uncovered lines:")
		for file, lines := range uncovered {
			ranges := diffcoverage.GroupLinesIntoRanges(lines)
			fmt.Printf("\tFile: %s\n", file)
			for _, r := range ranges {
				if r[0] == r[1] {
					fmt.Printf("\t- %d\n", r[0])
				} else {
					fmt.Printf("\t- %d-%d\n", r[0], r[1])
				}
			}
			fmt.Println()
		}
	}

	fmt.Printf("New/Changed lines coverage in functions: %.2f%%\n", coveragePercent)
}
