package tn

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// GetAbsPath resolves data file paths using the caller's source location.
// Deprecated: use language-specific paths (ChineseDataPath, EnglishDataPath, etc.)
// for production builds. This function may fail in cross-compiled binaries.
func GetAbsPath(relPath string) string {
	_, filename, _, _ := runtime.Caller(1)
	dir := filepath.Dir(filename)

	for {
		if strings.HasSuffix(dir, "rules") {
			parent := filepath.Dir(dir)
			return filepath.Join(parent, relPath)
		}
		if strings.HasSuffix(dir, "tn") || strings.HasSuffix(dir, "itn") {
			return filepath.Join(dir, "chinese", relPath)
		}
		if strings.HasSuffix(dir, "debug") || strings.HasSuffix(dir, "examples") {
			return filepath.Join("/Users/liuzanhuang/git-telen/go/github.com/TelenLiu/WeTextProcessing-go/tn/chinese", relPath)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return filepath.Join(dir, relPath)
}

// ChineseDataPath resolves a data file path for the Chinese language module.
// Tries embedded data first (production), falls back to development path.
func ChineseDataPath(relPath string) string {
	if p := tryEmbedPath("chinese", relPath); p != "" {
		return p
	}
	return developmentDataPath("tn/chinese", relPath)
}

// EnglishDataPath resolves a data file path for the English language module.
func EnglishDataPath(relPath string) string {
	if p := tryEmbedPath("english", relPath); p != "" {
		return p
	}
	return developmentDataPath("tn/english", relPath)
}

// JapaneseDataPath resolves a data file path for the Japanese language module.
func JapaneseDataPath(relPath string) string {
	if p := tryEmbedPath("japanese", relPath); p != "" {
		return p
	}
	return developmentDataPath("tn/japanese", relPath)
}

// ITNChineseDataPath resolves a data file path for the ITN Chinese module.
func ITNChineseDataPath(relPath string) string {
	if p := tryEmbedPath("itn/chinese", relPath); p != "" {
		return p
	}
	return developmentDataPath("itn/chinese", relPath)
}

// ITNJapaneseDataPath resolves a data file path for the ITN Japanese module.
func ITNJapaneseDataPath(relPath string) string {
	if p := tryEmbedPath("itn/japanese", relPath); p != "" {
		return p
	}
	return developmentDataPath("itn/japanese", relPath)
}

func tryEmbedPath(module, relPath string) string {
	root := ExtractDir()
	if root == "" {
		return ""
	}
	p := filepath.Join(root, module, relPath)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

func developmentDataPath(module, relPath string) string {
	_, filename, _, _ := runtime.Caller(2)
	base := filepath.Dir(filename)
	for {
		if strings.HasSuffix(base, module) {
			return filepath.Join(base, relPath)
		}
		parent := filepath.Dir(base)
		if parent == base {
			break
		}
		base = parent
	}
	return filepath.Join(filepath.Dir(filename), relPath)
}

func LoadLabels(absPath string) ([][]string, error) {
	file, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var labels [][]string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "\t")
		labels = append(labels, parts)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return labels, nil
}

func AugmentLabelsWithPunctAtEnd(labels [][]string) [][]string {
	var res [][]string
	for _, label := range labels {
		if len(label) > 1 {
			if len(label[0]) > 0 && label[0][len(label[0])-1] == '.' &&
				len(label[1]) > 0 && label[1][len(label[1])-1] != '.' {
				newLabel := []string{label[0], label[1] + "."}
				if len(label) > 2 {
					newLabel = append(newLabel, label[2:]...)
				}
				res = append(res, newLabel)
			}
		}
	}
	return res
}
