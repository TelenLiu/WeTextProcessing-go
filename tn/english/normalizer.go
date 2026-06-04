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
	startLabels map[int32]bool // ilabels from the tagger start state (for fast skip)
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

	// Cached token parser for verbalizer (avoids per-call allocation)
	tokenParser *tn.TokenParser

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
		Processor:    tn.NewProcessor("en_normalizer", "en_tn"),
		tokenParser:  tn.NewTokenParser("en_tn"),
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
		{"fraction", func() { n.fractionRule = rules.NewFraction() }},
		{"date", func() { n.dateRule = rules.NewDate() }},
		{"time", func() { n.timeRule = rules.NewTime() }},
		{"money", func() { n.moneyRule = rules.NewMoney() }},
		{"telephone", func() { n.telephoneRule = rules.NewTelephone() }},
		{"whitelist", func() { n.whitelistRule = rules.NewWhitelist() }},
		{"range", func() { n.rangeRule = rules.NewRange() }},
		// measure and electronic cause OOM due to VCHAR.Star().Compose() state explosion
		// {"measure", func() { n.measureRule = rules.NewMeasure() }},
		// {"electronic", func() { n.electronicRule = rules.NewElectronic() }},
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

	// Sort arcs for efficient binary search in composition
	sortArcs := func(f *pynini.Fst) {
		if f != nil && len(f.States) > 0 {
			f.ArcSort("input")
			f.PrepareForComposition()
		}
	}
	sortArcs(n.cardinalRule.Tagger)
	sortArcs(n.cardinalRule.Verbalizer)
	sortArcs(n.ordinalRule.Tagger)
	sortArcs(n.ordinalRule.Verbalizer)
	sortArcs(n.decimalRule.Tagger)
	sortArcs(n.decimalRule.Verbalizer)
	sortArcs(n.fractionRule.Tagger)
	sortArcs(n.fractionRule.Verbalizer)
	sortArcs(n.dateRule.Tagger)
	sortArcs(n.dateRule.Verbalizer)
	sortArcs(n.timeRule.Tagger)
	sortArcs(n.timeRule.Verbalizer)
	sortArcs(n.moneyRule.Tagger)
	sortArcs(n.moneyRule.Verbalizer)
	sortArcs(n.telephoneRule.Tagger)
	sortArcs(n.telephoneRule.Verbalizer)
	sortArcs(n.whitelistRule.Tagger)
	sortArcs(n.whitelistRule.Verbalizer)
	sortArcs(n.rangeRule.Tagger)
	sortArcs(n.rangeRule.Verbalizer)
	sortArcs(n.wordRule.Tagger)
	sortArcs(n.wordRule.Verbalizer)
	sortArcs(n.punctRule.Tagger)
	sortArcs(n.punctRule.Verbalizer)

	// Build ordered rule list for greedy matching.
	// Lower weight = higher priority. Matching Python's add_weight ordering.
	n.rules = []ruleEntry{
		{"date", n.dateRule.Tagger, n.dateRule.Verbalizer, 0.99, nil},
		{"cardinal", n.cardinalRule.Tagger, n.cardinalRule.Verbalizer, 1.0, nil},
		{"ordinal", n.ordinalRule.Tagger, n.ordinalRule.Verbalizer, 1.0, nil},
		{"decimal", n.decimalRule.Tagger, n.decimalRule.Verbalizer, 1.0, nil},
		{"fraction", n.fractionRule.Tagger, n.fractionRule.Verbalizer, 1.0, nil},
		{"time", n.timeRule.Tagger, n.timeRule.Verbalizer, 1.0, nil},
		{"money", n.moneyRule.Tagger, n.moneyRule.Verbalizer, 1.0, nil},
		{"telephone", n.telephoneRule.Tagger, n.telephoneRule.Verbalizer, 1.0, nil},
		{"whitelist", n.whitelistRule.Tagger, n.whitelistRule.Verbalizer, 1.0, nil},
		{"range", n.rangeRule.Tagger, n.rangeRule.Verbalizer, 1.01, nil},
		{"p", n.punctRule.Tagger, n.punctRule.Verbalizer, 2.0, nil},
		{"w", n.wordRule.Tagger, n.wordRule.Verbalizer, 100.0, nil},
	}

	// Pre-compute start labels for each rule's tagger FST.
	// These are the ilabels reachable from the start state (including epsilon closure),
	// used to quickly skip rules that cannot match at a given position.
	for i := range n.rules {
		n.rules[i].startLabels = computeStartLabels(n.rules[i].tagger)
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

// computeStartLabels returns the set of ilabels reachable from the start state
// of the given FST, including ilabels reachable via epsilon transitions.
// This is used for a fast "can this rule possibly match?" check.
func computeStartLabels(fst *pynini.Fst) map[int32]bool {
	if fst == nil || len(fst.States) == 0 {
		return nil
	}
	labels := make(map[int32]bool)
	visited := make(map[int32]bool)
	var queue []int32
	queue = append(queue, fst.Start)
	visited[fst.Start] = true

	for len(queue) > 0 {
		sid := queue[0]
		queue = queue[1:]
		state := &fst.States[sid]
		for i := range state.Arcs {
			arc := &state.Arcs[i]
			if arc.ILabel != pynini.EpsilonLabel {
				labels[arc.ILabel] = true
			} else if !visited[arc.Next] {
				visited[arc.Next] = true
				queue = append(queue, arc.Next)
			}
		}
	}
	return labels
}

// findBestMatch tries all rules at the given position and returns the best match.
// Optimized with:
//   - startLabels fast-path to skip rules that can't match at this position
//   - Limited search depth (maxLen) to avoid processing very long strings
func (n *Normalizer) findBestMatch(runes []rune, pos int) *matchResult {
	// Limit search depth: most English TN matches are < 40 characters
	const maxLen = 50
	end := len(runes)
	if end-pos > maxLen {
		end = pos + maxLen
	}
	remaining := string(runes[pos:end])
	escaped := pynini.Escape(remaining)

	var best *matchResult

	for _, r := range n.rules {
		if r.tagger == nil || len(r.tagger.States) == 0 {
			continue
		}

		// Fast-path: skip rules whose start state can't match the first character.
		// Compute the first character's label using this rule's own symbol table.
		if len(r.startLabels) > 0 {
			firstLabel := r.tagger.FindRuneLabel(runes[pos])
			if firstLabel >= 0 && !r.startLabels[firstLabel] {
				continue
			}
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
	reordered := n.tokenParser.Reorder(tagOutput)
	return pynini.ComposeInputWithFst(pynini.Escape(reordered), nil, verbalizer)
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
