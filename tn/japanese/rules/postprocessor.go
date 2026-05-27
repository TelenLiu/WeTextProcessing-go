package rules

import (
	"os"
	"strings"

	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type PostProcessor struct {
	remove_interjections bool
	remove_puncts        bool
	tag_oov              bool

	blacklist map[string]bool
	puncts    map[string]bool
	charset   map[string]bool
}

func NewPostProcessor(remove_interjections, remove_puncts, tag_oov bool) *PostProcessor {
	p := &PostProcessor{
		remove_interjections: remove_interjections,
		remove_puncts:        remove_puncts,
		tag_oov:              tag_oov,
	}

	if remove_interjections {
		p.blacklist = loadStringSet(tn.JapaneseDataPath("data/default/blacklist.tsv"))
	}

	if remove_puncts || tag_oov {
		p.puncts = loadStringSet(tn.JapaneseDataPath("data/char/punctuations_ja.tsv"))
	}

	if tag_oov {
		// 片假名/平假名/浊音/半浊音/小写假名
		stdChars := loadStringSet(tn.JapaneseDataPath("data/char/hiragana_and_katakana.tsv"))
		// 日语常用汉字表
		extChars := loadStringSet(tn.JapaneseDataPath("data/char/common_chinese_char.tsv"))

		p.charset = make(map[string]bool)
		for k := range stdChars {
			p.charset[k] = true
		}
		for k := range extChars {
			p.charset[k] = true
		}
		// Add punctuations, digits, alpha, space to charset
		for k := range p.puncts {
			p.charset[k] = true
		}
		// ASCII digits
		for i := '0'; i <= '9'; i++ {
			p.charset[string(i)] = true
		}
		// ASCII letters
		for i := 'a'; i <= 'z'; i++ {
			p.charset[string(i)] = true
		}
		for i := 'A'; i <= 'Z'; i++ {
			p.charset[string(i)] = true
		}
		// ASCII punctuation marks (matching Python byte.PUNCT)
		for _, r := range []rune{
			'!', '"', '#', '$', '%', '&', '\'', '(', ')', '*', '+',
			',', '-', '.', '/', ':', ';', '<', '=', '>', '?', '@',
			'[', '\\', ']', '^', '_', '`', '{', '|', '}', '~',
		} {
			p.charset[string(r)] = true
		}
		p.charset[" "] = true
		p.charset["\t"] = true
		p.charset["\n"] = true
	}

	return p
}

func loadStringSet(path string) map[string]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return make(map[string]bool)
	}

	set := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		set[parts[0]] = true
	}
	return set
}

func (p *PostProcessor) isKnownChar(ch string) bool {
	if p.charset == nil {
		return true
	}
	return p.charset[ch]
}

func (p *PostProcessor) Apply(input string) string {
	if input == "" {
		return input
	}

	var b strings.Builder
	b.Grow(len(input) * 3)

	for _, ch := range input {
		chStr := string(ch)

		// Remove interjections (blacklist)
		if p.remove_interjections && p.blacklist[chStr] {
			continue
		}

		// Remove punctuations
		if p.remove_puncts && p.puncts[chStr] {
			continue
		}

		// Tag OOV characters
		if p.tag_oov && !p.isKnownChar(chStr) {
			b.WriteString("<oov>")
			b.WriteString(chStr)
			b.WriteString("</oov>")
			continue
		}

		b.WriteString(chStr)
	}

	return b.String()
}
