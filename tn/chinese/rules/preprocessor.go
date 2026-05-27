package rules

import (
	"os"
	"strings"

	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type PreProcessor struct {
	traditional_to_simple bool
	// Lookup map for traditional -> simple character replacement
	replacements map[string]string
}

func NewPreProcessor(traditional_to_simple bool) *PreProcessor {
	p := &PreProcessor{
		traditional_to_simple: traditional_to_simple,
	}

	if traditional_to_simple {
		p.replacements = loadTraditionalToSimple()
	}

	return p
}

func loadTraditionalToSimple() map[string]string {
	absPath := tn.ChineseDataPath("data/char/traditional_to_simple.tsv")
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}

	replacements := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 2 {
			replacements[parts[0]] = parts[1]
		} else if len(parts) == 1 {
			// Identity mapping
			replacements[parts[0]] = parts[0]
		}
	}
	return replacements
}

// Apply applies traditional to simple conversion if enabled
func (p *PreProcessor) Apply(input string) string {
	if !p.traditional_to_simple || p.replacements == nil {
		return input
	}

	// Build output character by character
	var b strings.Builder
	b.Grow(len(input) * 3) // rough estimate

	for _, ch := range input {
		chStr := string(ch)
		if repl, ok := p.replacements[chStr]; ok {
			b.WriteString(repl)
		} else {
			b.WriteString(chStr)
		}
	}
	return b.String()
}
