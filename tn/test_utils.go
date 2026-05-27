package tn

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type TestCase struct {
	Input    string
	Expected string
}

func ParseTestCase(filePath string) ([]TestCase, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var testCases []TestCase
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "=>", 2)
		if len(parts) != 2 {
			continue
		}

		testCases = append(testCases, TestCase{
			Input:    strings.TrimSpace(parts[0]),
			Expected: strings.TrimSpace(parts[1]),
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return testCases, nil
}

func GetTestDataPath(relativePath string) string {
	basePath, err := os.Getwd()
	if err != nil {
		basePath = "."
	}
	return filepath.Join(basePath, relativePath)
}
