package pynini

import (
	"bufio"
	"os"
	"strings"
)

// StringFile reads a TSV file and creates a union FST from the mappings.
// Each line: input\toutput or just input (for acceptor).
func StringFile(path string) (*Fst, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var fsts []*Fst
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "\t")
		if len(parts) >= 2 {
			fsts = append(fsts, Cross(parts[0], parts[1]))
		} else if len(parts) == 1 {
			fsts = append(fsts, Accep(parts[0]))
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return Union(fsts...), nil
}

// StringFileMust reads a TSV file and creates a union FST, panicking on error.
func StringFileMust(path string) *Fst {
	fst, err := StringFile(path)
	if err != nil {
		return NewFst()
	}
	return fst
}

// StringMap creates a union FST from a slice of string pairs.
func StringMap(mappings [][]string) *Fst {
	var fsts []*Fst
	for _, pair := range mappings {
		if len(pair) >= 2 {
			fsts = append(fsts, Cross(pair[0], pair[1]))
		} else if len(pair) == 1 {
			fsts = append(fsts, Accep(pair[0]))
		}
	}
	return Union(fsts...)
}