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
	name        string
	tagger      *pynini.Fst
	verbalizer  *pynini.Fst
	weight      float32
	startLabels map[int32]bool // ilabels from the tagger start state (for fast skip)
	triggerSet  map[rune]bool  // pre-computed rune set for fast trigger check
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

	// Pre-computed set of runes that can trigger any rule
	triggerRunes map[rune]bool

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
		{"measure", func() { n.measureRule = rules.NewMeasure() }},
		{"electronic", func() { n.electronicRule = rules.NewElectronic() }},
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

	// Sort arcs for efficient binary search in composition.
	// Apply RmEpsilon to eliminate epsilon arcs from tagger FSTs,
	// which dramatically reduces epsilon closure BFS cost at runtime.
	// Only apply to FSTs with < 10000 states to avoid OOM during RmEpsilon.
	sortArcs := func(f *pynini.Fst) {
		if f != nil && len(f.States) > 0 {
			f.ArcSort("input")
			f.PrepareForComposition()
		}
	}
	optimizeTagger := func(f *pynini.Fst) *pynini.Fst {
		if f == nil || len(f.States) == 0 {
			return f
		}
		if len(f.States) > 10000 {
			// Too large for RmEpsilon (causes arc explosion), just sort and prepare
			f.ArcSort("input")
			f.PrepareForComposition()
			return f
		}
		// Apply RmEpsilon to eliminate epsilon transitions
		optimized := f.RmEpsilon().Connect()
		optimized.ArcSort("input")
		optimized.PrepareForComposition()
		return optimized
	}
	optimizeVerbalizer := func(f *pynini.Fst) *pynini.Fst {
		if f == nil || len(f.States) == 0 {
			return f
		}
		if len(f.States) > 10000 {
			f.ArcSort("input")
			f.PrepareForComposition()
			return f
		}
		optimized := f.RmEpsilon().Connect()
		optimized.ArcSort("input")
		optimized.PrepareForComposition()
		return optimized
	}
	sortArcs(n.cardinalRule.Tagger)
	n.cardinalRule.Tagger = optimizeTagger(n.cardinalRule.Tagger)
	sortArcs(n.cardinalRule.Verbalizer)
	n.cardinalRule.Verbalizer = optimizeVerbalizer(n.cardinalRule.Verbalizer)
	sortArcs(n.ordinalRule.Tagger)
	n.ordinalRule.Tagger = optimizeTagger(n.ordinalRule.Tagger)
	sortArcs(n.ordinalRule.Verbalizer)
	n.ordinalRule.Verbalizer = optimizeVerbalizer(n.ordinalRule.Verbalizer)
	sortArcs(n.decimalRule.Tagger)
	n.decimalRule.Tagger = optimizeTagger(n.decimalRule.Tagger)
	sortArcs(n.decimalRule.Verbalizer)
	n.decimalRule.Verbalizer = optimizeVerbalizer(n.decimalRule.Verbalizer)
	sortArcs(n.fractionRule.Tagger)
	n.fractionRule.Tagger = optimizeTagger(n.fractionRule.Tagger)
	sortArcs(n.fractionRule.Verbalizer)
	n.fractionRule.Verbalizer = optimizeVerbalizer(n.fractionRule.Verbalizer)
	sortArcs(n.dateRule.Tagger)
	n.dateRule.Tagger = optimizeTagger(n.dateRule.Tagger)
	sortArcs(n.dateRule.Verbalizer)
	n.dateRule.Verbalizer = optimizeVerbalizer(n.dateRule.Verbalizer)
	sortArcs(n.timeRule.Tagger)
	n.timeRule.Tagger = optimizeTagger(n.timeRule.Tagger)
	sortArcs(n.timeRule.Verbalizer)
	n.timeRule.Verbalizer = optimizeVerbalizer(n.timeRule.Verbalizer)
	sortArcs(n.moneyRule.Tagger)
	n.moneyRule.Tagger = optimizeTagger(n.moneyRule.Tagger)
	sortArcs(n.moneyRule.Verbalizer)
	n.moneyRule.Verbalizer = optimizeVerbalizer(n.moneyRule.Verbalizer)
	sortArcs(n.telephoneRule.Tagger)
	n.telephoneRule.Tagger = optimizeTagger(n.telephoneRule.Tagger)
	sortArcs(n.telephoneRule.Verbalizer)
	n.telephoneRule.Verbalizer = optimizeVerbalizer(n.telephoneRule.Verbalizer)
	sortArcs(n.whitelistRule.Tagger)
	n.whitelistRule.Tagger = optimizeTagger(n.whitelistRule.Tagger)
	sortArcs(n.whitelistRule.Verbalizer)
	n.whitelistRule.Verbalizer = optimizeVerbalizer(n.whitelistRule.Verbalizer)
	sortArcs(n.rangeRule.Tagger)
	n.rangeRule.Tagger = optimizeTagger(n.rangeRule.Tagger)
	sortArcs(n.rangeRule.Verbalizer)
	n.rangeRule.Verbalizer = optimizeVerbalizer(n.rangeRule.Verbalizer)
	sortArcs(n.wordRule.Tagger)
	n.wordRule.Tagger = optimizeTagger(n.wordRule.Tagger)
	sortArcs(n.wordRule.Verbalizer)
	n.wordRule.Verbalizer = optimizeVerbalizer(n.wordRule.Verbalizer)
	sortArcs(n.punctRule.Tagger)
	n.punctRule.Tagger = optimizeTagger(n.punctRule.Tagger)
	sortArcs(n.punctRule.Verbalizer)
	n.punctRule.Verbalizer = optimizeVerbalizer(n.punctRule.Verbalizer)
	sortArcs(n.measureRule.Tagger)
	n.measureRule.Tagger = optimizeTagger(n.measureRule.Tagger)
	sortArcs(n.measureRule.Verbalizer)
	n.measureRule.Verbalizer = optimizeVerbalizer(n.measureRule.Verbalizer)
	sortArcs(n.electronicRule.Tagger)
	n.electronicRule.Tagger = optimizeTagger(n.electronicRule.Tagger)
	sortArcs(n.electronicRule.Verbalizer)
	n.electronicRule.Verbalizer = optimizeVerbalizer(n.electronicRule.Verbalizer)

	// Release base FSTs from each rule's Processor — they are only needed
	// during BuildTagger/BuildVerbalizer, not at runtime.
	n.cardinalRule.ReleaseBaseFsts()
	n.ordinalRule.ReleaseBaseFsts()
	n.decimalRule.ReleaseBaseFsts()
	n.fractionRule.ReleaseBaseFsts()
	n.dateRule.ReleaseBaseFsts()
	n.timeRule.ReleaseBaseFsts()
	n.moneyRule.ReleaseBaseFsts()
	n.telephoneRule.ReleaseBaseFsts()
	n.whitelistRule.ReleaseBaseFsts()
	n.rangeRule.ReleaseBaseFsts()
	n.wordRule.ReleaseBaseFsts()
	n.punctRule.ReleaseBaseFsts()
	n.measureRule.ReleaseBaseFsts()
	n.electronicRule.ReleaseBaseFsts()

	// Release the Normalizer's own Processor base FSTs too — they are
	// only used during construction, not at runtime.
	n.Processor.ReleaseBaseFsts()

	// Build ordered rule list for greedy matching.
	// Lower weight = higher priority. Matching Python's add_weight ordering.
	// Note: "w" (word) rule is omitted because it's an identity mapping —
	// unmatched characters are already output as-is by the fallback logic
	// in Normalize(). Including word would cause every letter to trigger
	// a compose call, making triggerRunes optimization useless.
	n.rules = []ruleEntry{
		{"date", n.dateRule.Tagger, n.dateRule.Verbalizer, 0.99, nil, nil},
		{"cardinal", n.cardinalRule.Tagger, n.cardinalRule.Verbalizer, 1.0, nil, nil},
		{"ordinal", n.ordinalRule.Tagger, n.ordinalRule.Verbalizer, 1.0, nil, nil},
		{"decimal", n.decimalRule.Tagger, n.decimalRule.Verbalizer, 1.0, nil, nil},
		{"fraction", n.fractionRule.Tagger, n.fractionRule.Verbalizer, 1.0, nil, nil},
		{"time", n.timeRule.Tagger, n.timeRule.Verbalizer, 1.0, nil, nil},
		{"money", n.moneyRule.Tagger, n.moneyRule.Verbalizer, 1.0, nil, nil},
		{"measure", n.measureRule.Tagger, n.measureRule.Verbalizer, 1.0, nil, nil},
		{"electronic", n.electronicRule.Tagger, n.electronicRule.Verbalizer, 1.0, nil, nil},
		{"telephone", n.telephoneRule.Tagger, n.telephoneRule.Verbalizer, 1.0, nil, nil},
		{"whitelist", n.whitelistRule.Tagger, n.whitelistRule.Verbalizer, 1.0, nil, nil},
		{"range", n.rangeRule.Tagger, n.rangeRule.Verbalizer, 1.01, nil, nil},
		{"p", n.punctRule.Tagger, n.punctRule.Verbalizer, 2.0, nil, nil},
	}

	// Pre-compute start labels for each rule's tagger FST.
	// These are the ilabels reachable from the start state (including epsilon closure),
	// used to quickly skip rules that cannot match at a given position.
	for i := range n.rules {
		n.rules[i].startLabels = computeStartLabels(n.rules[i].tagger)
		// Pre-compute triggerSet: map[rune]bool for fast trigger check
		// without needing FindRuneLabel call at runtime.
		if n.rules[i].tagger != nil && n.rules[i].tagger.Symbols != nil && len(n.rules[i].startLabels) > 0 {
			ts := make(map[rune]bool, len(n.rules[i].startLabels))
			for label := range n.rules[i].startLabels {
				sym := n.rules[i].tagger.Symbols.Symbol(label)
				if rns := []rune(sym); len(rns) == 1 {
					ts[rns[0]] = true
				}
			}
			// For electronic rule, add all alphanumeric characters to trigger set.
			// The electronic tagger uses Compose with VCHAR* which creates epsilon
			// arcs that RmEpsilon may not fully resolve. Emails/URLs can start with
			// any letter or digit.
			if n.rules[i].name == "electronic" {
				for c := 'a'; c <= 'z'; c++ {
					ts[c] = true
					ts[c-'a'+'A'] = true
				}
				for c := '0'; c <= '9'; c++ {
					ts[c] = true
				}
			}
			n.rules[i].triggerSet = ts
		}
	}
	sort.SliceStable(n.rules, func(i, j int) bool {
		return n.rules[i].weight < n.rules[j].weight
	})

	// Pre-compute trigger runes: union of all rules' start label symbols.
	n.triggerRunes = make(map[rune]bool, 128)
	for _, r := range n.rules {
		if r.tagger == nil || r.tagger.Symbols == nil {
			continue
		}
		for label := range r.startLabels {
			sym := r.tagger.Symbols.Symbol(label)
			if rns := []rune(sym); len(rns) == 1 {
				n.triggerRunes[rns[0]] = true
			}
		}
	}

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
	isPunct    bool     // true if this is a punctuation match
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
		// Fast-path: if the current character can't trigger any rule,
		// output it as-is without trying any composition.
		if runes[pos] == ' ' {
			result.WriteRune(' ')
			pos++
			continue
		}

		if len(n.triggerRunes) > 0 && !n.triggerRunes[runes[pos]] {
			result.WriteRune(runes[pos])
			pos++
			continue
		}

		// Try to match each rule at the current position
		best := n.findBestMatch(runes, pos)
		if best != nil && best.inputLen > 0 {
			// Apply verbalizer to the tagged output
			verbalized := n.applyVerbalizer(best.tagOutput, best.verbalizer, best.ruleName)
			if verbalized != "" {
				result.WriteString(verbalized)
			}
			pos += best.inputLen
		} else {
			// No rule matched; output the character as-is
			result.WriteRune(runes[pos])
			pos++
		}
	}

	output := result.String()
	// Clean up multiple spaces - O(n) single-pass
	output = collapseSpaces(output)
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
//   - Direct rune processing to avoid string allocation
func (n *Normalizer) findBestMatch(runes []rune, pos int) *matchResult {
	// Limit search depth: most English TN matches are < 30 characters
	maxLen := 30
	remaining := len(runes) - pos
	if remaining < maxLen {
		maxLen = remaining
	}

	var best *matchResult

	for i, r := range n.rules {
		if r.tagger == nil || len(r.tagger.States) == 0 {
			continue
		}

		// Fast-path: skip rules whose start state can't match the first character.
		// Use pre-computed triggerSet for O(1) rune lookup without FindRuneLabel.
		if len(r.triggerSet) > 0 && !r.triggerSet[runes[pos]] {
			continue
		}

		// Search depth: use current best match length to limit search,
		// but only if best match already covers the remaining input.
		// This avoids the issue where a short cardinal match prevents
		// a longer fraction/range/time match from being found.
		searchLen := maxLen
		if best != nil && best.inputLen >= remaining {
			// Already matched the entire remaining input, no need to search deeper
			searchLen = best.inputLen
		}

		result := pynini.ComposePrefixShortestPathRunes(runes, pos, searchLen, r.tagger)
		if result.Consumed == 0 || result.Output == "" {
			continue
		}

		// Verify this rule actually matched (check if tag output contains the rule name)
		if !strings.Contains(result.Output, r.name) {
			continue
		}

		// Token boundary check: if the match ends in the middle of a word,
		// reject it. A word boundary means the character after the match
		// must not be alphanumeric when the last matched character is alphanumeric.
		// This prevents "Th" from matching inside "The", "Mon" inside "Money", etc.
		endPos := pos + result.Consumed
		if endPos < len(runes) && isWordChar(runes[endPos-1]) && isWordChar(runes[endPos]) {
			continue
		}

		candidate := &matchResult{
			ruleName:   r.name,
			inputLen:   result.Consumed,
			tagOutput:  result.Output,
			verbalizer: r.verbalizer,
			weight:     r.weight + result.Weight,
			isPunct:    r.name == "p",
		}

		// Prefer longer match; for same length, prefer lower weight
		if best == nil || candidate.inputLen > best.inputLen ||
			(candidate.inputLen == best.inputLen && candidate.weight < best.weight) {
			best = candidate
		}

		// Early termination: if we matched the entire remaining input,
		// check if the current best can be beaten by any remaining rule.
		// Only break if best.totalWeight < nextRule.baseWeight (no future rule can beat it).
		if best != nil && best.inputLen >= remaining {
			canBeat := false
			for j := i + 1; j < len(n.rules); j++ {
				nextRule := n.rules[j]
				if nextRule.tagger == nil || len(nextRule.tagger.States) == 0 {
					continue
				}
				// If a future rule's base weight is less than the best total weight,
				// it could potentially beat the current best (with a path weight of 0).
				// Also, triggerSet check: if the next rule can't even match the first char,
				// it can't beat the current best.
				if len(nextRule.triggerSet) > 0 && !nextRule.triggerSet[runes[pos]] {
					continue
				}
				if nextRule.weight < best.weight {
					canBeat = true
					break
				}
			}
			if !canBeat {
				break
			}
		}
	}

	return best
}

// applyVerbalizer applies the verbalizer to the tagged output.
// Uses fast-path string extraction for simple rules where the verbalizer
// just deletes field names and keeps values, falling back to FST composition
// for complex rules (date, time, fraction, range, ordinal, telephone, etc.).
func (n *Normalizer) applyVerbalizer(tagOutput string, verbalizer *pynini.Fst, ruleName string) string {
	if verbalizer == nil || len(verbalizer.States) == 0 {
		return ""
	}
	reordered := n.tokenParser.Reorder(tagOutput)

	// Fast-path: for rules with simple verbalizers that just extract field values
	// without transformation. This avoids expensive FST composition.
	// Rules that need FST: ordinal (suppletive), date, time, fraction, range (Insert ops),
	// telephone, electronic (complex formatting), whitelist (case conversion).
	switch ruleName {
	case "cardinal":
		if result := verbalizeCardinalFast(reordered); result != "" {
			return result
		}
	case "decimal":
		if result := verbalizeDecimalFast(reordered); result != "" {
			return result
		}
	case "measure":
		if result := verbalizeMeasureFast(reordered); result != "" {
			return result
		}
	case "money":
		if result := verbalizeMoneyFast(reordered); result != "" {
			return result
		}
	}

	return pynini.ComposeInputWithFst(reordered, nil, verbalizer)
}

// verbalizeCardinalFast extracts the integer value from a cardinal token.
// Also applies Python's one_to_a replacements: "one thousand" -> "thousand",
// "one hundred" -> "a hundred", "one million" -> "a million".
func verbalizeCardinalFast(reordered string) string {
	intVal := extractFieldValue(reordered, "integer")
	if intVal == "" {
		return ""
	}
	// Apply one_to_a replacements (Python cardinal.py lines 102-109)
	// These replacements are alternatives with higher weight in Python,
	// so they only apply when the replacement is shorter/better.
	// "one thousand" -> "thousand" is always preferred (2 words -> 1 word)
	// "one hundred" -> "a hundred" and "one million" -> "a million" are alternatives
	intVal = applyOneToAReplacements(intVal)

	if strings.Contains(reordered, "negative: \"true\"") {
		return "negative " + intVal
	}
	return intVal
}

// applyOneToAReplacements applies Python's one_to_a replacements.
// In Python, these are alternatives added via Compose with VCHAR.star.
// "one thousand" -> "thousand" is always preferred because it's shorter (2 words -> 1 word).
// "one hundred" -> "a hundred" and "one million" -> "a million" are alternatives
// that are NOT preferred in the test data (same length, higher weight).
func applyOneToAReplacements(s string) string {
	// "one thousand" -> "thousand" is always preferred
	if strings.HasPrefix(s, "one thousand") {
		s = "thousand" + s[len("one thousand"):]
	}
	return s
}

// verbalizeOrdinalFast extracts ordinal value.
func verbalizeOrdinalFast(reordered string) string {
	intVal := extractFieldValue(reordered, "integer")
	if intVal == "" {
		return ""
	}
	return intVal
}

// verbalizeDecimalFast extracts decimal number parts.
func verbalizeDecimalFast(reordered string) string {
	intVal := extractFieldValue(reordered, "integer")
	if intVal == "" {
		return ""
	}
	fracVal := extractFieldValue(reordered, "fractional")
	quantVal := extractFieldValue(reordered, "quantity")
	result := intVal
	if fracVal != "" {
		result += " point " + fracVal
	}
	if quantVal != "" {
		result += " " + quantVal
	}
	neg := extractFieldValue(reordered, "negative")
	if neg == "true" {
		result = "minus " + result
	}
	return result
}

// verbalizeMoneyFast extracts money value and currency.
// Handles: integer-only ($12), decimal ($12.05), integer+quantity ($1 million),
// decimal+quantity ($1.2 million), and trailing zero deletion.
// Matches Python test data format: "X point Y dollars" for decimal,
// "X dollars" for integer-only, "X million dollars" for quantity.
func verbalizeMoneyFast(reordered string) string {
	intVal := extractFieldValue(reordered, "integer_part")
	fracVal := extractFieldValue(reordered, "fractional_part")
	currencyVal := extractFieldValue(reordered, "currency_maj")
	quantityVal := extractFieldValue(reordered, "quantity")
	if intVal == "" {
		return ""
	}
	// Strip trailing " oh" and " zero" from fractional part (zero-deletion)
	// Matches Python's decimal_delete_last_zeros behavior
	fracVal = stripTrailingZeros(fracVal)

	result := intVal
	if fracVal != "" {
		result += " point " + fracVal
	}
	if quantityVal != "" {
		result += " " + quantityVal
	}
	if currencyVal != "" {
		result += " " + currencyVal
	}
	return result
}

// stripTrailingZeros removes trailing " oh" and " zero" from a fractional part string.
// "$12.0500" -> "oh five oh oh" -> "oh five"
// "$1.2320" -> "two three two oh" -> "two three two"
// "$12.05" -> "oh five" -> "oh five" (no change)
func stripTrailingZeros(s string) string {
	if s == "" {
		return s
	}
	// Repeatedly strip trailing " oh" and " zero"
	for {
		trimmed := false
		if strings.HasSuffix(s, " oh") {
			s = s[:len(s)-3]
			trimmed = true
		}
		if strings.HasSuffix(s, " zero") {
			s = s[:len(s)-5]
			trimmed = true
		}
		if !trimmed {
			break
		}
	}
	return s
}

// verbalizeMeasureFast extracts measure value and unit.
func verbalizeMeasureFast(reordered string) string {
	numVal := extractFieldValue(reordered, "value")
	if numVal == "" {
		return ""
	}
	unitVal := extractFieldValue(reordered, "unit")
	if unitVal != "" {
		return numVal + " " + unitVal
	}
	return numVal
}

// verbalizeTelephoneFast extracts telephone number parts.
func verbalizeTelephoneFast(reordered string) string {
	parts := extractQuotedContent(reordered)
	if parts == "" {
		return ""
	}
	return parts
}

// verbalizeElectronicFast extracts electronic format parts.
func verbalizeElectronicFast(reordered string) string {
	parts := extractQuotedContent(reordered)
	if parts == "" {
		return ""
	}
	return parts
}

// extractFieldValue extracts a field value from a token string.
// Format: field: "value"
func extractFieldValue(input, field string) string {
	prefix := field + ": \""
	idx := strings.Index(input, prefix)
	if idx < 0 {
		return ""
	}
	start := idx + len(prefix)
	end := strings.Index(input[start:], "\"")
	if end < 0 {
		return ""
	}
	return input[start : start+end]
}

// extractQuotedContent extracts all double-quoted strings from input and concatenates them.
func extractQuotedContent(input string) string {
	var result strings.Builder
	i := 0
	for i < len(input) {
		q := strings.Index(input[i:], "\"")
		if q < 0 {
			break
		}
		start := i + q + 1
		end := strings.Index(input[start:], "\"")
		if end < 0 {
			break
		}
		result.WriteString(input[start : start+end])
		i = start + end + 1
	}
	if result.Len() == 0 {
		return ""
	}
	return result.String()
}

// isWordChar returns true if the rune is a letter or digit.
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
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

// collapseSpaces replaces runs of multiple spaces with a single space in O(n).
func collapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			if !prevSpace {
				b.WriteByte(' ')
			}
			prevSpace = true
		} else {
			b.WriteByte(s[i])
			prevSpace = false
		}
	}
	return b.String()
}

// DebugRuleStats returns the number of states in each rule's tagger and verbalizer FSTs.
func (n *Normalizer) DebugRuleStats() []struct {
	Name          string
	TaggerStates  int
	VerbStates    int
	Weight        float32
	StartLabels   int
	NumIEps       int
	NumOEps       int
	NumArcs       int
} {
	var stats []struct {
		Name          string
		TaggerStates  int
		VerbStates    int
		Weight        float32
		StartLabels   int
		NumIEps       int
		NumOEps       int
		NumArcs       int
	}
	for _, r := range n.rules {
		ts, vs, sl := 0, 0, 0
		ieps, oeps, arcs := 0, 0, 0
		if r.tagger != nil {
			ts = len(r.tagger.States)
			for _, st := range r.tagger.States {
				ieps += int(st.NumIEps)
				oeps += int(st.NumOEps)
				arcs += len(st.Arcs)
			}
		}
		if r.verbalizer != nil {
			vs = len(r.verbalizer.States)
		}
		if r.startLabels != nil {
			sl = len(r.startLabels)
		}
		stats = append(stats, struct {
			Name          string
			TaggerStates  int
			VerbStates    int
			Weight        float32
			StartLabels   int
			NumIEps       int
			NumOEps       int
			NumArcs       int
		}{r.name, ts, vs, r.weight, sl, ieps, oeps, arcs})
	}
	return stats
}

// PrintStats prints memory and timing statistics.
func (n *Normalizer) PrintStats() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("Build time: %v, Memory: Alloc=%vMB, Sys=%vMB\n",
		n.buildTime, m.Alloc/1024/1024, m.Sys/1024/1024)
}
