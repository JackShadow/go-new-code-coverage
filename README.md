# Go New Code Coverage

**Go New Code Coverage** is a lightweight code that helps analyze test coverage for newly added or modified lines in Go codebases. It relies on native Go’s coverage reports (`cover.out`) and diff data (`diff.txt`) to determine if all relevant changes in `.go` files are properly tested. The tool also excludes the last line of each function body (usually the closing brace `}`) from coverage checks.

## Installation and Usage

Below is a sample Bash script demonstrating how to install the tool, generate a diff, and run the coverage analysis:

```bash
#!/bin/bash

# Install the Go New Code Coverage tool from GitHub
go install github.com/JackShadow/go-new-code-coverage@latest

# Generate a diff between your current branch and origin/main with zero context
git diff origin/main --unified=0 > diff.txt

# Run all Go tests with coverage and save the profile to cover.out
go test ./... -coverprofile=cover.out

# Run coverage analysis for newly added/changed lines in the repository
# -vvv (or --verbose) enables detailed output of uncovered lines
# -min=85.0 sets the minimum acceptable coverage threshold
go-new-code-coverage -vvv -min=85.0 cover.out diff.txt .
