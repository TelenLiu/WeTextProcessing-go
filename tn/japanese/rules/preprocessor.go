package rules

import (
	"os"
	"strings"

	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type PreProcessor struct {
	full_to_half bool
	replacements map[string]string
}

func NewPreProcessor(full_to_half bool) *PreProcessor {
	p := &PreProcessor{
		full_to_half: full_to_half,
	}

	if full_to_half {
		p.replacements = loadFullwidthToHalfwidth()
	}

	return p
}

func loadFullwidthToHalfwidth() map[string]string {
	absPath := tn.JapaneseDataPath("data/char/fullwidth_to_halfwidth.tsv")
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
			replacements[parts[0]] = parts[0]
		}
	}
	return replacements
}

func (p *PreProcessor) Apply(input string) string {
	if !p.full_to_half || p.replacements == nil {
		return input
	}

	var b strings.Builder
	b.Grow(len(input))

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
