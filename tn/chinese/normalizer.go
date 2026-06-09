package chinese

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

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
	triggerSet  map[rune]bool  // pre-computed rune set for fast trigger check
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

	// Pre-computed set of rune labels that can trigger any rule.
	// If a character's label is not in this set, we skip all rules immediately.
	triggerRunes map[rune]bool

	// Per-rule FST cache directory
	ruleCacheDir string
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
	return NewNormalizerEx(cache_dir, overwrite_cache,
		remove_interjections, remove_erhua, traditional_to_simple,
		remove_puncts, full_to_half, tag_oov,
		nil, progress...)
}

// NewNormalizerEx creates a Normalizer with an extended progress callback
// that includes estimated remaining time. progressEx is called in addition
// to any basic BuildProgressFn provided in the progress variadic args.
func NewNormalizerEx(
	cache_dir string,
	overwrite_cache bool,
	remove_interjections bool,
	remove_erhua bool,
	traditional_to_simple bool,
	remove_puncts bool,
	full_to_half bool,
	tag_oov bool,
	progressEx tn.BuildProgressExFn,
	progress ...tn.BuildProgressFn,
) *Normalizer {
	// Extend global VCHAR with CJK characters for Chinese text processing.
	// This must be done before any Processor is created (including rules).
	charsetPath := tn.ChineseDataPath("data/char/charset_national_standard_2013_8105.tsv")
	if charsetPath != "" {
		lib.ExtendValidUTF8Char(charsetPath)
	}
	n := &Normalizer{
		Processor:            tn.NewProcessorLazy("zh_normalizer"),
		remove_interjections: remove_interjections,
		remove_erhua:         remove_erhua,
		traditional_to_simple: traditional_to_simple,
		remove_puncts:        remove_puncts,
		full_to_half:         full_to_half,
		tag_oov:              tag_oov,
		tokenParser:          tn.NewTokenParser("tn"),
	}
	if cache_dir != "" {
		n.ruleCacheDir = filepath.Join(cache_dir, "zh_rules")
	}
	var pf tn.BuildProgressFn
	if len(progress) > 0 {
		pf = progress[0]
	}
	// Record build start time and set extended progress callback
	// before any build operations begin.
	n.Processor.SetBuildStartTime(time.Now())
	if progressEx != nil {
		n.Processor.SetBuildProgressEx(progressEx)
	}
	// Per-rule mode: only need base FST caching, not monolithic tagger/verbalizer.
	// Load or build base FSTs (VSIGMA, CHAR, SIGMA, etc.) from cache.
	n.Processor.InitBaseFstCache("zh_tn", cache_dir, overwrite_cache, pf)
	// Build per-rule FSTs (from cache or from scratch) — this is the only call.
	n.buildTaggerInternal()
	n.buildVerbalizerInternal()
	return n
}

func (n *Normalizer) BuildFst(prefix, cacheDir string, overwriteCache bool, concurrency int, progress tn.BuildProgressFn) {
	// Per-rule mode: pass no-op builders to BuildFstWithCache.
	// We only need base FST caching; per-rule FSTs are built separately
	// in buildTaggerInternal/buildVerbalizerInternal called from NewNormalizer.
	n.Processor.BuildFstWithCache(prefix, cacheDir, overwriteCache, concurrency, progress, func() {}, func() {})
}

// BuildTagger builds the tagger FST (kept for backward compatibility)
func (n *Normalizer) BuildTagger() {
	n.buildTaggerInternal()
}

func (n *Normalizer) buildTaggerInternal() {
	if n.ruleCacheDir != "" && n.checkRuleCacheExists() {
		n.loadRulesFromCache()
	} else {
		n.buildRulesFromScratch()
		if n.ruleCacheDir != "" {
			n.saveRulesToCache()
		}
	}

	// PreProcessor is not composed into FST
	n.preProcessor = rules.NewPreProcessor(n.traditional_to_simple)

	// Release base FSTs from each rule's Processor — they are only needed
	// during BuildTagger/BuildVerbalizer, not at runtime.
	n.cardinalRule.ReleaseBaseFsts()
	n.dateRule.ReleaseBaseFsts()
	n.whitelistRule.ReleaseBaseFsts()
	n.sportRule.ReleaseBaseFsts()
	n.fractionRule.ReleaseBaseFsts()
	n.measureRule.ReleaseBaseFsts()
	n.moneyRule.ReleaseBaseFsts()
	n.timeRule.ReleaseBaseFsts()
	n.mathRule.ReleaseBaseFsts()
	n.charRule.ReleaseBaseFsts()

	// Release the Normalizer's own Processor base FSTs too — they are
	// only used during construction, not at runtime.
	n.Processor.ReleaseBaseFsts()

	// Build ordered rule list for greedy matching.
	// Lower weight = higher priority. Matching Python's add_weight ordering.
	// Note: "char" rule is omitted because it's an identity mapping —
	// unmatched characters are already output as-is by the fallback logic
	// in Normalize(). Including char would cause every CJK character to
	// trigger a compose call, making triggerRunes optimization useless.
	n.zhRules = []zhRuleEntry{
		{"date", n.dateRule.Tagger, n.dateRule.Verbalizer, 1.02, nil, nil},
		{"whitelist", n.whitelistRule.Tagger, n.whitelistRule.Verbalizer, 1.03, nil, nil},
		{"sport", n.sportRule.Tagger, n.sportRule.Verbalizer, 1.04, nil, nil},
		{"fraction", n.fractionRule.Tagger, n.fractionRule.Verbalizer, 1.05, nil, nil},
		{"measure", n.measureRule.Tagger, n.measureRule.Verbalizer, 1.05, nil, nil},
		{"money", n.moneyRule.Tagger, n.moneyRule.Verbalizer, 1.05, nil, nil},
		{"time", n.timeRule.Tagger, n.timeRule.Verbalizer, 1.05, nil, nil},
		{"cardinal", n.cardinalRule.Tagger, n.cardinalRule.Verbalizer, 1.06, nil, nil},
		{"math", n.mathRule.Tagger, n.mathRule.Verbalizer, 90, nil, nil},
	}

	// Pre-compute start labels and trigger sets for each rule's tagger FST.
	for i := range n.zhRules {
		n.zhRules[i].startLabels = computeStartLabels(n.zhRules[i].tagger)
		if n.zhRules[i].tagger != nil && n.zhRules[i].tagger.Symbols != nil && len(n.zhRules[i].startLabels) > 0 {
			ts := make(map[rune]bool, len(n.zhRules[i].startLabels))
			for label := range n.zhRules[i].startLabels {
				sym := n.zhRules[i].tagger.Symbols.Symbol(label)
				if rns := []rune(sym); len(rns) == 1 {
					ts[rns[0]] = true
				}
			}
			n.zhRules[i].triggerSet = ts
		}
	}

	sort.SliceStable(n.zhRules, func(i, j int) bool {
		return n.zhRules[i].weight < n.zhRules[j].weight
	})

	// Pre-compute trigger runes: union of all rules' start label symbols.
	// Used for fast "can any rule match?" check at each position.
	n.triggerRunes = make(map[rune]bool, 64)
	for _, r := range n.zhRules {
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
}

// ruleCacheNames lists the rule names and their expected cache files.
var ruleCacheNames = []string{
	"cardinal", "date", "whitelist", "sport", "fraction",
	"measure", "money", "time", "math", "char",
}

// checkRuleCacheExists checks if all per-rule FST cache files exist in n.ruleCacheDir.
func (n *Normalizer) checkRuleCacheExists() bool {
	for _, name := range ruleCacheNames {
		taggerPath := filepath.Join(n.ruleCacheDir, name+"_tagger.fst")
		verbalizerPath := filepath.Join(n.ruleCacheDir, name+"_verbalizer.fst")
		if _, err := os.Stat(taggerPath); err != nil {
			return false
		}
		if _, err := os.Stat(verbalizerPath); err != nil {
			return false
		}
	}
	// Also check cardinal_number.fst
	numberPath := filepath.Join(n.ruleCacheDir, "cardinal_number.fst")
	if _, err := os.Stat(numberPath); err != nil {
		return false
	}
	return true
}

// loadRulesFromCache loads all rule Tagger/Verbalizer FSTs from disk cache.
// Cached FSTs are already optimized, so no RmEpsilon/Connect/ArcSort is needed.
func (n *Normalizer) loadRulesFromCache() {
	concurrency, _ := n.Processor.GetBuildConfig()

	type loadTask struct {
		name string
		fn   func()
	}
	tasks := []loadTask{
		{"cardinal", func() {
			n.cardinalRule = &rules.Cardinal{Processor: tn.NewProcessorLazy("cardinal")}
			n.cardinalRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "cardinal_tagger.fst"))
			n.cardinalRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "cardinal_verbalizer.fst"))
			number, _ := pynini.FstRead(filepath.Join(n.ruleCacheDir, "cardinal_number.fst"))
			n.cardinalRule.SetCachedNumber(number)
		}},
		{"date", func() {
			n.dateRule = &rules.Date{Processor: tn.NewProcessorLazy("date")}
			n.dateRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "date_tagger.fst"))
			n.dateRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "date_verbalizer.fst"))
		}},
		{"whitelist", func() {
			n.whitelistRule = rules.NewWhitelistEmpty(n.remove_erhua)
			n.whitelistRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "whitelist_tagger.fst"))
			n.whitelistRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "whitelist_verbalizer.fst"))
		}},
		{"sport", func() {
			n.sportRule = &rules.Sport{Processor: tn.NewProcessorLazy("sport")}
			n.sportRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "sport_tagger.fst"))
			n.sportRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "sport_verbalizer.fst"))
		}},
		{"fraction", func() {
			n.fractionRule = &rules.Fraction{Processor: tn.NewProcessorLazy("fraction")}
			n.fractionRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "fraction_tagger.fst"))
			n.fractionRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "fraction_verbalizer.fst"))
		}},
		{"measure", func() {
			n.measureRule = &rules.Measure{Processor: tn.NewProcessorLazy("measure")}
			n.measureRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "measure_tagger.fst"))
			n.measureRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "measure_verbalizer.fst"))
		}},
		{"money", func() {
			n.moneyRule = &rules.Money{Processor: tn.NewProcessorLazy("money")}
			n.moneyRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "money_tagger.fst"))
			n.moneyRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "money_verbalizer.fst"))
		}},
		{"time", func() {
			n.timeRule = &rules.Time{Processor: tn.NewProcessorLazy("time")}
			n.timeRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "time_tagger.fst"))
			n.timeRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "time_verbalizer.fst"))
		}},
		{"char", func() {
			n.charRule = &rules.Char{Processor: tn.NewProcessorLazy("char")}
			n.charRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "char_tagger.fst"))
			n.charRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "char_verbalizer.fst"))
		}},
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, t := range tasks {
		wg.Add(1)
		go func(task loadTask, idx int) {
			defer wg.Done()
			sem <- struct{}{}
			task.fn()
			<-sem
			n.Processor.ReportProgress("加载缓存-"+task.name, idx+1, len(tasks)+1)
		}(t, i)
	}
	wg.Wait()

	// math depends on cardinal
	n.mathRule = &rules.Math{Processor: tn.NewProcessorLazy("math")}
	n.mathRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "math_tagger.fst"))
	n.mathRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "math_verbalizer.fst"))
	n.Processor.ReportProgress("加载缓存-math", len(tasks)+1, len(tasks)+1)
}

// buildRulesFromScratch builds all rule FSTs from scratch (the original logic).
func (n *Normalizer) buildRulesFromScratch() {
	concurrency, _ := n.Processor.GetBuildConfig()

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
			n.Processor.ReportProgress("构建Tagger-"+task.name, idx+1, len(independent)+1)
		}(t, i)
	}
	wg.Wait()

	// mathRule depends on cardinalRule
	n.mathRule = rules.NewMathWithCardinal(n.cardinalRule)
	n.Processor.ReportProgress("构建Tagger-math", len(independent)+1, len(independent)+1)

	// Sort arcs for efficient binary search in composition.
	// Apply RmEpsilon to eliminate epsilon arcs from tagger FSTs,
	// which dramatically reduces epsilon closure BFS cost at runtime.
	// RmEpsilon has built-in arc explosion protection, so we can apply
	// it to all FSTs regardless of size.
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
		// RmEpsilon has built-in arc explosion protection.
		// Always apply it to eliminate epsilon transitions.
		optimized := f.RmEpsilon().Connect()
		optimized.ArcSort("input")
		optimized.PrepareForComposition()
		return optimized
	}
	optimizeVerbalizer := func(f *pynini.Fst) *pynini.Fst {
		if f == nil || len(f.States) == 0 {
			return f
		}
		optimized := f.RmEpsilon().Connect()
		optimized.ArcSort("input")
		optimized.PrepareForComposition()
		return optimized
	}
	// Optimize all tagger and verbalizer FSTs.
	// This is the most time-consuming step after building rules.
	type optItem struct {
		name      string
		tagger    **pynini.Fst
		verbalizer **pynini.Fst
	}
	optItems := []optItem{
		{"cardinal", &n.cardinalRule.Tagger, &n.cardinalRule.Verbalizer},
		{"date", &n.dateRule.Tagger, &n.dateRule.Verbalizer},
		{"whitelist", &n.whitelistRule.Tagger, &n.whitelistRule.Verbalizer},
		{"sport", &n.sportRule.Tagger, &n.sportRule.Verbalizer},
		{"fraction", &n.fractionRule.Tagger, &n.fractionRule.Verbalizer},
		{"measure", &n.measureRule.Tagger, &n.measureRule.Verbalizer},
		{"money", &n.moneyRule.Tagger, &n.moneyRule.Verbalizer},
		{"time", &n.timeRule.Tagger, &n.timeRule.Verbalizer},
		{"math", &n.mathRule.Tagger, &n.mathRule.Verbalizer},
		{"char", &n.charRule.Tagger, &n.charRule.Verbalizer},
	}
	for i, item := range optItems {
		sortArcs(*item.tagger)
		*item.tagger = optimizeTagger(*item.tagger)
		sortArcs(*item.verbalizer)
		*item.verbalizer = optimizeVerbalizer(*item.verbalizer)
		n.Processor.ReportProgress("优化-"+item.name, i+1, len(optItems))
	}
}

// saveRulesToCache saves all per-rule FSTs to disk cache.
func (n *Normalizer) saveRulesToCache() {
	if err := os.MkdirAll(n.ruleCacheDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to create rule cache dir %s: %v\n", n.ruleCacheDir, err)
		return
	}

	type saveItem struct {
		name string
		fst  *pynini.Fst
	}
	items := []saveItem{
		{"cardinal_tagger", n.cardinalRule.Tagger},
		{"cardinal_verbalizer", n.cardinalRule.Verbalizer},
		{"cardinal_number", n.cardinalRule.Number},
		{"date_tagger", n.dateRule.Tagger},
		{"date_verbalizer", n.dateRule.Verbalizer},
		{"whitelist_tagger", n.whitelistRule.Tagger},
		{"whitelist_verbalizer", n.whitelistRule.Verbalizer},
		{"sport_tagger", n.sportRule.Tagger},
		{"sport_verbalizer", n.sportRule.Verbalizer},
		{"fraction_tagger", n.fractionRule.Tagger},
		{"fraction_verbalizer", n.fractionRule.Verbalizer},
		{"measure_tagger", n.measureRule.Tagger},
		{"measure_verbalizer", n.measureRule.Verbalizer},
		{"money_tagger", n.moneyRule.Tagger},
		{"money_verbalizer", n.moneyRule.Verbalizer},
		{"time_tagger", n.timeRule.Tagger},
		{"time_verbalizer", n.timeRule.Verbalizer},
		{"math_tagger", n.mathRule.Tagger},
		{"math_verbalizer", n.mathRule.Verbalizer},
		{"char_tagger", n.charRule.Tagger},
		{"char_verbalizer", n.charRule.Verbalizer},
	}

	for _, item := range items {
		if item.fst == nil {
			continue
		}
		path := filepath.Join(n.ruleCacheDir, item.name+".fst")
		if err := pynini.FstWrite(item.fst, path); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write rule cache %s: %v\n", path, err)
		}
	}
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

// GetMeasureTagger returns the measure rule's tagger FST for analysis.
func (n *Normalizer) GetMeasureTagger() *pynini.Fst {
	if n.measureRule == nil {
		return nil
	}
	return n.measureRule.Tagger
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
		// Fast-path: if the current character can't trigger any rule,
		// output it as-is without trying any composition.
		if len(n.triggerRunes) > 0 && !n.triggerRunes[runes[pos]] {
			result.WriteRune(runes[pos])
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
// Optimized with startLabels fast-path, limited search depth, and direct rune processing.
func (n *Normalizer) findBestMatch(runes []rune, pos int) *zhMatchResult {
	// Limit search depth: most Chinese TN matches are < 15 characters
	// (longest is 11-digit phone number)
	maxLen := 20
	remaining := len(runes) - pos
	if remaining < maxLen {
		maxLen = remaining
	}

	var best *zhMatchResult

	for i, r := range n.zhRules {
		if r.tagger == nil || len(r.tagger.States) == 0 {
			continue
		}

		// Fast-path: skip rules whose start state can't match the first character.
		// Use pre-computed triggerSet for O(1) rune lookup without FindRuneLabel.
		if len(r.triggerSet) > 0 && !r.triggerSet[runes[pos]] {
			continue
		}

		// Search depth: limit to maxLen, but only reduce if best covers remaining input
		searchLen := maxLen
		if best != nil && best.inputLen >= remaining {
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

		// Early termination: if we matched the entire remaining input,
		// check if the current best can be beaten by any remaining rule.
		// Only break if best.totalWeight < nextRule.baseWeight (no future rule can beat it).
		if best != nil && best.inputLen >= remaining {
			canBeat := false
			for j := i + 1; j < len(n.zhRules); j++ {
				nextRule := n.zhRules[j]
				if nextRule.tagger == nil || len(nextRule.tagger.States) == 0 {
					continue
				}
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
// Uses fast-path string extraction for all rules, avoiding expensive
// FST composition that causes state explosion with DeleteString operations.
func (n *Normalizer) applyVerbalizer(tagOutput string, verbalizer *pynini.Fst) string {
	if verbalizer == nil || len(verbalizer.States) == 0 {
		return ""
	}
	reordered := n.tokenParser.Reorder(tagOutput)

	// Extract token name
	tokenName := ""
	if idx := strings.Index(reordered, " { "); idx > 0 {
		tokenName = reordered[:idx]
	}

	// Fast-path verbalization for all Chinese rules.
	// The verbalizer logic is simple: extract field values and combine them
	// with optional insertions. This avoids the FST composition state explosion
	// caused by DeleteString operations creating millions of oEps arcs.
	switch tokenName {
	case "fraction":
		return verbalizeFractionFast(reordered)
	case "date":
		return verbalizeDateFast(reordered)
	case "time":
		return verbalizeTimeFast(reordered)
	case "whitelist":
		return verbalizeWhitelistFast(reordered, n.remove_erhua)
	default:
		// For other rules (cardinal, money, measure, sport, math, char),
		// extract quoted content directly.
		if result := extractQuotedContent(reordered); result != "" {
			return result
		}
	}

	// Fallback to FST composition (should rarely be reached)
	escaped := pynini.Escape(reordered)
	result := pynini.ComposeInputWithFst(escaped, nil, verbalizer)
	return result
}

// verbalizeFractionFast implements the fraction verbalizer as string operations.
// Python: denominator + insert("分之") + numerator
// Input format: fraction { denominator: "五" numerator: "一" }
// Output: 五分之一
func verbalizeFractionFast(reordered string) string {
	denominator := extractFieldValue(reordered, "denominator")
	numerator := extractFieldValue(reordered, "numerator")
	if denominator == "" || numerator == "" {
		return extractQuotedContent(reordered)
	}
	return denominator + "分之" + numerator
}

// verbalizeDateFast implements the date verbalizer as string operations.
// Python: year.Ques() + month + day.Ques()
// Input format: date { year: "二零零二年" month: "一月" day: "二十八日" }
// Output: 二零零二年一月二十八日
func verbalizeDateFast(reordered string) string {
	year := extractFieldValue(reordered, "year")
	month := extractFieldValue(reordered, "month")
	day := extractFieldValue(reordered, "day")
	if month == "" {
		return extractQuotedContent(reordered)
	}
	result := year + month
	if day != "" {
		result += day
	}
	return result
}

// verbalizeTimeFast implements the time verbalizer as string operations.
// Python: noon.Ques() + hour + minute + second.Ques()
// Input format: time { hour: "五点" minute: "零二分" } or time { noon: "上午" hour: "八点" }
// Output: 五点零二分 or 上午八点
func verbalizeTimeFast(reordered string) string {
	noon := extractFieldValue(reordered, "noon")
	hour := extractFieldValue(reordered, "hour")
	minute := extractFieldValue(reordered, "minute")
	second := extractFieldValue(reordered, "second")
	if hour == "" {
		return extractQuotedContent(reordered)
	}
	result := noon + hour + minute
	if second != "" {
		result += second
	}
	return result
}

// verbalizeWhitelistFast implements the whitelist verbalizer as string operations.
// Python: if remove_erhua, delete 'erhua: "儿"' entirely; otherwise keep "儿".
// Also handles normal whitelist values: value: "xxx" -> "xxx"
func verbalizeWhitelistFast(reordered string, removeErhua bool) string {
	// Check for erhua field
	erhua := extractFieldValue(reordered, "erhua")
	if erhua != "" {
		if removeErhua {
			return "" // Delete "儿" entirely
		}
		return erhua // Keep "儿"
	}
	// Normal whitelist: extract value field
	value := extractFieldValue(reordered, "value")
	if value != "" {
		return value
	}
	return extractQuotedContent(reordered)
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
// Returns "" if no quoted content found.
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

// DebugRuleStats returns the number of states in each rule's tagger and verbalizer FSTs.
func (n *Normalizer) DebugRuleStats() []struct {
	Name         string
	TaggerStates int
	VerbStates   int
	Weight       float32
	StartLabels  int
} {
	var stats []struct {
		Name         string
		TaggerStates int
		VerbStates   int
		Weight       float32
		StartLabels  int
	}
	for _, r := range n.zhRules {
		ts, vs, sl := 0, 0, 0
		if r.tagger != nil {
			ts = len(r.tagger.States)
		}
		if r.verbalizer != nil {
			vs = len(r.verbalizer.States)
		}
		if r.startLabels != nil {
			sl = len(r.startLabels)
		}
		stats = append(stats, struct {
			Name         string
			TaggerStates int
			VerbStates   int
			Weight       float32
			StartLabels  int
		}{r.name, ts, vs, r.weight, sl})
	}
	return stats
}

// DebugPerRuleTiming measures per-rule composition timing for a given input.
func (n *Normalizer) DebugPerRuleTiming(input string) []struct {
	Name     string
	States   int
	Duration time.Duration
	Consumed int
	Output   string
} {
	runes := []rune(input)
	var results []struct {
		Name     string
		States   int
		Duration time.Duration
		Consumed int
		Output   string
	}
	for _, r := range n.zhRules {
		if r.tagger == nil || len(r.tagger.States) == 0 {
			continue
		}
		if len(r.triggerSet) > 0 && !r.triggerSet[runes[0]] {
			continue
		}
		states := len(r.tagger.States)
		// Warmup
		for i := 0; i < 3; i++ {
			pynini.ComposePrefixShortestPathRunes(runes, 0, 20, r.tagger)
		}
		iters := 50
		start := time.Now()
		var result pynini.ComposePrefixResult
		for i := 0; i < iters; i++ {
			result = pynini.ComposePrefixShortestPathRunes(runes, 0, 20, r.tagger)
		}
		elapsed := time.Since(start) / time.Duration(iters)
		results = append(results, struct {
			Name     string
			States   int
			Duration time.Duration
			Consumed int
			Output   string
		}{r.name, states, elapsed, result.Consumed, result.Output})
	}
	return results
}

// Close releases resources held by the Normalizer, including stopping
// background cache eviction goroutines. After calling Close, the Normalizer
// should not be used for further Normalize calls. It is safe to call
// Close multiple times.
func (n *Normalizer) Close() {
	n.Processor.Close()
}
