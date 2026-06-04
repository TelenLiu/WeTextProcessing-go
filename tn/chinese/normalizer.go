package chinese

import (
	"sort"
	"strings"
	"sync"

	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
	"github.com/TelenLiu/WeTextProcessing-go/tn/chinese/rules"
)

// ruleEntry holds a rule's tagger, verbalizer, name, and weight for greedy matching.
type zhRuleEntry struct {
	name        string
	tagger      *pynini.Fst
	verbalizer  *pynini.Fst
	weight      float32
	startLabels map[int32]bool // ilabels from the tagger start state (for fast skip)
}

type Normalizer struct {
	*tn.Processor

	remove_interjections   bool
	remove_erhua          bool
	traditional_to_simple bool
	remove_puncts         bool
	full_to_half          bool
	tag_oov               bool

	preProcessor  *rules.PreProcessor
	postProcessor *rules.PostProcessor

	// Rule instances shared between tagger and verbalizer
	cardinalRule  *rules.Cardinal
	dateRule      *rules.Date
	whitelistRule *rules.Whitelist
	sportRule     *rules.Sport
	fractionRule  *rules.Fraction
	measureRule   *rules.Measure
	moneyRule     *rules.Money
	timeRule      *rules.Time
	mathRule      *rules.Math
	charRule      *rules.Char

	// Ordered list of rules for greedy matching (higher priority first)
	zhRules []zhRuleEntry

	// Cached token parser for verbalizer
	tokenParser *tn.TokenParser
}

func NewNormalizer(
	cache_dir string,
	overwrite_cache bool,
	remove_interjections bool,
	remove_erhua bool,
	traditional_to_simple bool,
	remove_puncts bool,
	full_to_half bool,
	tag_oov bool,
	progress ...tn.BuildProgressFn,
) *Normalizer {
	// Extend global VCHAR with CJK characters for Chinese text processing.
	// This must be done before any Processor is created (including rules).
	charsetPath := tn.ChineseDataPath("data/char/charset_national_standard_2013_8105.tsv")
	if charsetPath != "" {
		lib.ExtendValidUTF8Char(charsetPath)
	}
	n := &Normalizer{
		Processor:            tn.NewProcessor("zh_normalizer"),
		remove_interjections: remove_interjections,
		remove_erhua:         remove_erhua,
		traditional_to_simple: traditional_to_simple,
		remove_puncts:        remove_puncts,
		full_to_half:         full_to_half,
		tag_oov:              tag_oov,
		tokenParser:          tn.NewTokenParser("tn"),
	}
	var pf tn.BuildProgressFn
	if len(progress) > 0 {
		pf = progress[0]
	}
	n.BuildFst("zh_tn", cache_dir, overwrite_cache, 0, pf)
	return n
}

func (n *Normalizer) BuildFst(prefix, cacheDir string, overwriteCache bool, concurrency int, progress tn.BuildProgressFn) {
	n.Processor.BuildFstWithCache(prefix, cacheDir, overwriteCache, concurrency, progress, n.buildTaggerInternal, n.buildVerbalizerInternal)
}

// BuildTagger builds the tagger FST (kept for backward compatibility)
func (n *Normalizer) BuildTagger() {
	n.buildTaggerInternal()
}

func (n *Normalizer) buildTaggerInternal() {
	concurrency, progress := n.Processor.GetBuildConfig()

	type ruleTask struct {
		name string
		fn   func()
	}
	// All rules except math (depends on cardinal)
	independent := []ruleTask{
		{"cardinal", func() { n.cardinalRule = rules.NewCardinal() }},
		{"date", func() { n.dateRule = rules.NewDate() }},
		{"whitelist", func() { n.whitelistRule = rules.NewWhitelist(n.remove_erhua) }},
		{"sport", func() { n.sportRule = rules.NewSport() }},
		{"fraction", func() { n.fractionRule = rules.NewFraction() }},
		{"measure", func() { n.measureRule = rules.NewMeasure() }},
		{"money", func() { n.moneyRule = rules.NewMoney() }},
		{"time", func() { n.timeRule = rules.NewTime() }},
		{"char", func() { n.charRule = rules.NewChar() }},
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, t := range independent {
		wg.Add(1)
		go func(task ruleTask, idx int) {
			defer wg.Done()
			sem <- struct{}{}
			task.fn()
			<-sem
			if progress != nil {
				progress("构建Tagger-"+task.name, idx+1, len(independent)+1)
			}
		}(t, i)
	}
	wg.Wait()

	// mathRule depends on cardinalRule
	n.mathRule = rules.NewMathWithCardinal(n.cardinalRule)
	if progress != nil {
		progress("构建Tagger-math", len(independent)+1, len(independent)+1)
	}

	// PreProcessor is not composed into FST
	n.preProcessor = rules.NewPreProcessor(n.traditional_to_simple)

	// Sort arcs for efficient binary search in composition
	sortArcs := func(f *pynini.Fst) {
		if f != nil && len(f.States) > 0 {
			f.ArcSort("input")
			f.PrepareForComposition()
		}
	}
	sortArcs(n.cardinalRule.Tagger)
	sortArcs(n.cardinalRule.Verbalizer)
	sortArcs(n.dateRule.Tagger)
	sortArcs(n.dateRule.Verbalizer)
	sortArcs(n.whitelistRule.Tagger)
	sortArcs(n.whitelistRule.Verbalizer)
	sortArcs(n.sportRule.Tagger)
	sortArcs(n.sportRule.Verbalizer)
	sortArcs(n.fractionRule.Tagger)
	sortArcs(n.fractionRule.Verbalizer)
	sortArcs(n.measureRule.Tagger)
	sortArcs(n.measureRule.Verbalizer)
	sortArcs(n.moneyRule.Tagger)
	sortArcs(n.moneyRule.Verbalizer)
	sortArcs(n.timeRule.Tagger)
	sortArcs(n.timeRule.Verbalizer)
	sortArcs(n.mathRule.Tagger)
	sortArcs(n.mathRule.Verbalizer)
	sortArcs(n.charRule.Tagger)
	sortArcs(n.charRule.Verbalizer)

	// Build ordered rule list for greedy matching.
	// Lower weight = higher priority. Matching Python's add_weight ordering.
	n.zhRules = []zhRuleEntry{
		{"date", n.dateRule.Tagger, n.dateRule.Verbalizer, 1.02, nil},
		{"whitelist", n.whitelistRule.Tagger, n.whitelistRule.Verbalizer, 1.03, nil},
		{"sport", n.sportRule.Tagger, n.sportRule.Verbalizer, 1.04, nil},
		{"fraction", n.fractionRule.Tagger, n.fractionRule.Verbalizer, 1.05, nil},
		{"measure", n.measureRule.Tagger, n.measureRule.Verbalizer, 1.05, nil},
		{"money", n.moneyRule.Tagger, n.moneyRule.Verbalizer, 1.05, nil},
		{"time", n.timeRule.Tagger, n.timeRule.Verbalizer, 1.05, nil},
		{"cardinal", n.cardinalRule.Tagger, n.cardinalRule.Verbalizer, 1.06, nil},
		{"math", n.mathRule.Tagger, n.mathRule.Verbalizer, 90, nil},
		{"char", n.charRule.Tagger, n.charRule.Verbalizer, 100, nil},
	}

	// Pre-compute start labels for each rule's tagger FST.
	for i := range n.zhRules {
		n.zhRules[i].startLabels = computeStartLabels(n.zhRules[i].tagger)
	}
	sort.SliceStable(n.zhRules, func(i, j int) bool {
		return n.zhRules[i].weight < n.zhRules[j].weight
	})

	// Set minimal tagger/verbalizer for Processor interface compatibility
	n.Tagger = pynini.NewFst()
	n.Verbalizer = pynini.NewFst()
}

// BuildVerbalizer builds the verbalizer FST (kept for backward compatibility)
func (n *Normalizer) BuildVerbalizer() {
	n.buildVerbalizerInternal()
}

func (n *Normalizer) buildVerbalizerInternal() {
	// Verbalizers are already built per-rule in buildTaggerInternal.
	// PostProcessor is applied at runtime.
	n.postProcessor = rules.NewPostProcessor(
		n.remove_interjections,
		n.remove_puncts,
		n.full_to_half,
		n.tag_oov,
	)
}

// zhMatchResult holds the result of a greedy match attempt.
type zhMatchResult struct {
	ruleName   string
	inputLen   int          // number of input runes consumed
	tagOutput  string       // tagged output string
	verbalizer *pynini.Fst  // verbalizer for this rule
	weight     float32      // rule weight
}

// Normalize applies full text normalization pipeline using greedy matching.
// At each position, it tries all rules and picks the longest match
// with the lowest weight.
func (n *Normalizer) Normalize(input string) string {
	if len(input) == 0 {
		return ""
	}

	// Apply preprocessor first
	if n.preProcessor != nil {
		input = n.preProcessor.Apply(input)
	}

	runes := []rune(input)
	var result strings.Builder
	pos := 0

	for pos < len(runes) {
		// Try to match each rule at the current position
		best := n.findBestMatch(runes, pos)
		if best != nil && best.inputLen > 0 {
			// Apply verbalizer to the tagged output
			verbalized := n.applyVerbalizer(best.tagOutput, best.verbalizer)
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
	// Apply postprocessor
	if n.postProcessor != nil {
		output = n.postProcessor.Apply(output)
	}
	return output
}

// computeStartLabels returns the set of ilabels reachable from the start state
// of the given FST, including ilabels reachable via epsilon transitions.
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
// Optimized with startLabels fast-path and limited search depth.
func (n *Normalizer) findBestMatch(runes []rune, pos int) *zhMatchResult {
	// Limit search depth: most Chinese TN matches are < 30 characters
	const maxLen = 50
	end := len(runes)
	if end-pos > maxLen {
		end = pos + maxLen
	}
	remaining := string(runes[pos:end])
	escaped := pynini.Escape(remaining)

	var best *zhMatchResult

	for _, r := range n.zhRules {
		if r.tagger == nil || len(r.tagger.States) == 0 {
			continue
		}

		// Fast-path: skip rules whose start state can't match the first character.
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

		candidate := &zhMatchResult{
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
