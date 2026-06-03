package english

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
	"github.com/TelenLiu/WeTextProcessing-go/tn/english/rules"
)

// ruleEntry holds a rule's tagger, verbalizer, name, and weight for greedy matching.
type ruleEntry struct {
	name       string
	tagger     *pynini.Fst
	verbalizer *pynini.Fst
	weight     float32
}

// Normalizer performs English text normalization using greedy runtime matching
// instead of building a monolithic tagger/verbalizer FST (which causes state explosion).
type Normalizer struct {
	*tn.Processor

	// Rule instances shared between tagger and verbalizer
	cardinalRule   *rules.Cardinal
	ordinalRule    *rules.Ordinal
	decimalRule    *rules.Decimal
	fractionRule   *rules.Fraction
	dateRule       *rules.Date
	timeRule       *rules.Time
	measureRule    *rules.Measure
	moneyRule      *rules.Money
	telephoneRule  *rules.Telephone
	electronicRule *rules.Electronic
	wordRule       *rules.Word
	whitelistRule  *rules.Whitelist
	punctRule      *rules.Punctuation
	rangeRule      *rules.Range

	// Ordered list of rules for greedy matching (higher priority first)
	rules []ruleEntry

	// Build timing
	buildTime time.Duration
}

func NewNormalizer(
	cacheDir string,
	overwriteCache bool,
	progress ...tn.BuildProgressFn,
) *Normalizer {
	// English TN doesn't need CJK characters; reset to avoid performance overhead
	lib.ResetCJKVCHAR()
	n := &Normalizer{
		Processor: tn.NewProcessor("en_normalizer", "en_tn"),
	}
	var pf tn.BuildProgressFn
	if len(progress) > 0 {
		pf = progress[0]
	}
	n.BuildFst("en_tn", cacheDir, overwriteCache, 0, pf)
	return n
}

func (n *Normalizer) BuildFst(prefix, cacheDir string, overwriteCache bool, concurrency int, progress tn.BuildProgressFn) {
	n.Processor.BuildFstWithCache(prefix, cacheDir, overwriteCache, concurrency, progress, n.buildTaggerInternal, n.buildVerbalizerInternal)
}

func (n *Normalizer) BuildTagger() {
	n.buildTaggerInternal()
}

func (n *Normalizer) buildTaggerInternal() {
	t0 := time.Now()
	concurrency, progress := n.Processor.GetBuildConfig()
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	type task struct{ name string; fn func() }
	tasks := []task{
		{"cardinal", func() { n.cardinalRule = rules.NewCardinal() }},
		{"word", func() { n.wordRule = rules.NewWord() }},
		{"punct", func() { n.punctRule = rules.NewPunctuation() }},
		{"ordinal", func() { n.ordinalRule = rules.NewOrdinal() }},
		{"decimal", func() { n.decimalRule = rules.NewDecimal() }},
		// These rules are built lazily on first use to reduce initial build time and memory
		// {"fraction", func() { n.fractionRule = rules.NewFraction() }},
		// {"date", func() { n.dateRule = rules.NewDate() }},
		// {"time", func() { n.timeRule = rules.NewTime() }},
		// {"measure", func() { n.measureRule = rules.NewMeasure() }},
		// {"money", func() { n.moneyRule = rules.NewMoney() }},
		// {"telephone", func() { n.telephoneRule = rules.NewTelephone() }},
		// {"electronic", func() { n.electronicRule = rules.NewElectronic() }},
		// {"whitelist", func() { n.whitelistRule = rules.NewWhitelist() }},
		// {"range", func() { n.rangeRule = rules.NewRange() }},
	}
	for i, t := range tasks {
		wg.Add(1)
		go func(tt task, idx int) {
			defer wg.Done()
			sem <- struct{}{}
			tt.fn()
			<-sem
			if progress != nil {
				progress("构建Tagger-"+tt.name, idx+1, len(tasks))
			}
		}(t, i)
	}
	wg.Wait()

	// Build ordered rule list for greedy matching.
	// Lower weight = higher priority. Word rule has very high weight (fallback).
	// Punctuation has higher weight than numbers to prefer number matches.
	n.rules = []ruleEntry{
		{"cardinal", n.cardinalRule.Tagger, n.cardinalRule.Verbalizer, 1.0},
		{"ordinal", n.ordinalRule.Tagger, n.ordinalRule.Verbalizer, 1.0},
		{"decimal", n.decimalRule.Tagger, n.decimalRule.Verbalizer, 1.0},
		{"punct", n.punctRule.Tagger, n.punctRule.Verbalizer, 2.0},
		{"word", n.wordRule.Tagger, n.wordRule.Verbalizer, 100.0},
	}
	sort.SliceStable(n.rules, func(i, j int) bool {
		return n.rules[i].weight < n.rules[j].weight
	})

	// Set minimal tagger/verbalizer for Processor interface compatibility
	n.Tagger = pynini.NewFst()
	n.Verbalizer = pynini.NewFst()

	n.buildTime = time.Since(t0)
	if progress != nil {
		progress("完成", len(tasks), len(tasks))
	}
}

func (n *Normalizer) BuildVerbalizer() {
	n.buildVerbalizerInternal()
}

func (n *Normalizer) buildVerbalizerInternal() {
	// Verbalizers are already built per-rule in buildTaggerInternal.
	// No need to build a monolithic verbalizer FST.
}

// matchResult holds the result of a greedy match attempt.
type matchResult struct {
	ruleName   string
	inputLen   int      // number of input runes consumed
	tagOutput  string   // tagged output string
	verbalizer *pynini.Fst // verbalizer for this rule
	weight     float32  // rule weight
}

// Normalize applies full text normalization using greedy matching.
// At each position, it tries all rules and picks the longest match
// with the lowest weight.
func (n *Normalizer) Normalize(input string) string {
	if len(input) == 0 {
		return ""
	}

	runes := []rune(input)
	var result strings.Builder
	pos := 0

	for pos < len(runes) {
		// Skip leading spaces
		if runes[pos] == ' ' {
			result.WriteRune(' ')
			pos++
			continue
		}

		// Try to match each rule at the current position
		best := n.findBestMatch(runes, pos)
		if best != nil && best.inputLen > 0 {
			// Apply verbalizer to the tagged output
			verbalized := n.applyVerbalizer(best.tagOutput, best.verbalizer)
			if verbalized != "" {
				result.WriteString(verbalized)
			}
			pos += best.inputLen
			// Add space between tokens (unless at end or next is punctuation)
			if pos < len(runes) && runes[pos] != ' ' {
				// Check if next char is punctuation
				nextStr := string(runes[pos])
				if !n.isPunctuation(nextStr) {
					result.WriteString(" ")
				}
			}
		} else {
			// No rule matched; output the character as-is
			result.WriteRune(runes[pos])
			pos++
		}
	}

	output := result.String()
	// Clean up multiple spaces
	for strings.Contains(output, "  ") {
		output = strings.ReplaceAll(output, "  ", " ")
	}
	output = strings.TrimRight(output, " ")
	return output
}

// findBestMatch tries all rules at the given position and returns the best match.
// Uses ComposePrefixShortestPath for efficient prefix matching.
func (n *Normalizer) findBestMatch(runes []rune, pos int) *matchResult {
	remaining := string(runes[pos:])
	escaped := pynini.Escape(remaining)

	var best *matchResult

	for _, r := range n.rules {
		if r.tagger == nil || len(r.tagger.States) == 0 {
			continue
		}

		result := pynini.ComposePrefixShortestPath(escaped, r.tagger)
		if result.Consumed == 0 || result.Output == "" {
			continue
		}

		// Verify this rule actually matched (check if tag output contains the rule name)
		if !strings.Contains(result.Output, r.name) {
			continue
		}

		candidate := &matchResult{
			ruleName:   r.name,
			inputLen:   result.Consumed,
			tagOutput:  result.Output,
			verbalizer: r.verbalizer,
			weight:     r.weight + result.Weight,
		}

		// Prefer longer match; for same length, prefer lower weight
		if best == nil || candidate.inputLen > best.inputLen ||
			(candidate.inputLen == best.inputLen && candidate.weight < best.weight) {
			best = candidate
		}
	}

	return best
}

// applyVerbalizer applies the verbalizer to the tagged output.
func (n *Normalizer) applyVerbalizer(tagOutput string, verbalizer *pynini.Fst) string {
	if verbalizer == nil || len(verbalizer.States) == 0 {
		return ""
	}
	// Reorder tokens for the verbalizer
	reordered := tn.NewTokenParser("en_tn").Reorder(tagOutput)
	// Do NOT escape the tag output - it contains " characters that are part of
	// the tag format and must be matched literally by the verbalizer.
	input := pynini.Accep(reordered)
	return input.ComposeShortestPath(verbalizer)
}

// isPunctuation checks if a string is a single punctuation character.
func (n *Normalizer) isPunctuation(s string) bool {
	if len(s) != 1 {
		return false
	}
	r := []rune(s)[0]
	return r == '.' || r == ',' || r == '!' || r == '?' || r == ';' || r == ':' ||
		r == '\'' || r == '"' || r == ')' || r == ']' || r == '}'
}

// BuildTime returns the time spent building the normalizer.
func (n *Normalizer) BuildTime() time.Duration {
	return n.buildTime
}

// PrintStats prints memory and timing statistics.
func (n *Normalizer) PrintStats() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("Build time: %v, Memory: Alloc=%vMB, Sys=%vMB\n",
		n.buildTime, m.Alloc/1024/1024, m.Sys/1024/1024)
}
