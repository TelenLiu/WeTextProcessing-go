package english

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
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
	minMatchLen int            // minimum input length for this rule to match
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

	// Per-rule FST cache directory
	ruleCacheDir string

	// Whether to overwrite existing per-rule cache
	overwriteRuleCache bool

	// Build timing
	buildTime time.Duration
}

func NewNormalizer(
	cacheDir string,
	overwriteCache bool,
	progress ...tn.BuildProgressFn,
) *Normalizer {
	return NewNormalizerEx(cacheDir, overwriteCache, nil, progress...)
}

// NewNormalizerEx creates a Normalizer with an extended progress callback
// that includes estimated remaining time. progressEx is called in addition
// to any basic BuildProgressFn provided in the progress variadic args.
func NewNormalizerEx(
	cacheDir string,
	overwriteCache bool,
	progressEx tn.BuildProgressExFn,
	progress ...tn.BuildProgressFn,
) *Normalizer {
	// Note: Do NOT call ResetCJKVCHAR() here. It clears the global cjkVCHAR
	// which can race with concurrent Chinese normalizer initialization,
	// causing Chinese rule Processors to lose CJK character support.
	// Instead, English rules use their own Processor's SIGMA which is
	// built from the English VCHAR (without CJK).
	n := &Normalizer{
		Processor:    tn.NewProcessorLazy("en_normalizer", "en_tn"),
		tokenParser:  tn.NewTokenParser("en_tn"),
	}
	if cacheDir != "" {
		n.ruleCacheDir = filepath.Join(cacheDir, "en_rules")
	}
	n.overwriteRuleCache = overwriteCache
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
	n.Processor.InitBaseFstCache("en_tn", cacheDir, overwriteCache, pf)
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

func (n *Normalizer) BuildTagger() {
	n.buildTaggerInternal()
}

func (n *Normalizer) buildTaggerInternal() {
	t0 := time.Now()

	if n.ruleCacheDir != "" && !n.overwriteRuleCache && n.checkRuleCacheExists() {
		n.loadRulesFromCache()
	} else {
		n.buildRulesFromScratch()
		if n.ruleCacheDir != "" {
			n.saveRulesToCache()
		}
	}

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
		{name: "date", tagger: n.dateRule.Tagger, verbalizer: n.dateRule.Verbalizer, weight: 0.99},
		{name: "cardinal", tagger: n.cardinalRule.Tagger, verbalizer: n.cardinalRule.Verbalizer, weight: 1.0},
		{name: "ordinal", tagger: n.ordinalRule.Tagger, verbalizer: n.ordinalRule.Verbalizer, weight: 1.0},
		{name: "decimal", tagger: n.decimalRule.Tagger, verbalizer: n.decimalRule.Verbalizer, weight: 1.0},
		{name: "fraction", tagger: n.fractionRule.Tagger, verbalizer: n.fractionRule.Verbalizer, weight: 1.0},
		{name: "time", tagger: n.timeRule.Tagger, verbalizer: n.timeRule.Verbalizer, weight: 1.0},
		{name: "money", tagger: n.moneyRule.Tagger, verbalizer: n.moneyRule.Verbalizer, weight: 1.0},
		{name: "measure", tagger: n.measureRule.Tagger, verbalizer: n.measureRule.Verbalizer, weight: 1.0},
		{name: "electronic", tagger: n.electronicRule.Tagger, verbalizer: n.electronicRule.Verbalizer, weight: 1.0},
		{name: "telephone", tagger: n.telephoneRule.Tagger, verbalizer: n.telephoneRule.Verbalizer, weight: 1.0},
		{name: "whitelist", tagger: n.whitelistRule.Tagger, verbalizer: n.whitelistRule.Verbalizer, weight: 1.0},
		{name: "range", tagger: n.rangeRule.Tagger, verbalizer: n.rangeRule.Verbalizer, weight: 1.01},
		{name: "p", tagger: n.punctRule.Tagger, verbalizer: n.punctRule.Verbalizer, weight: 2.0},
	}

	// Pre-compute startLabels, triggerSet, and minMatchLen for each rule's tagger FST.
	for i := range n.rules {
		n.rules[i].startLabels = computeStartLabels(n.rules[i].tagger)
		n.rules[i].minMatchLen = computeMinMatchLen(n.rules[i].tagger)
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
	n.Processor.ReportProgress("完成", 14, 14)
}

// enRuleCacheNames lists the English rule names and their expected cache files.
var enRuleCacheNames = []string{
	"cardinal", "word", "punct", "ordinal", "decimal", "fraction",
	"date", "time", "money", "telephone", "whitelist", "range",
	"measure", "electronic",
}

// enRuleCacheExtraFiles lists additional intermediate FST files beyond
// the standard {name}_tagger.fst and {name}_verbalizer.fst patterns.
var enRuleCacheExtraFiles = []string{
	"cardinal_graph",
	"cardinal_graph_hundred",
	"cardinal_single_digits",
	"cardinal_long_numbers",
	"punct_graph",
}

// checkRuleCacheExists checks if all per-rule FST cache files exist in n.ruleCacheDir.
func (n *Normalizer) checkRuleCacheExists() bool {
	for _, name := range enRuleCacheNames {
		taggerPath := filepath.Join(n.ruleCacheDir, name+"_tagger.fst")
		verbalizerPath := filepath.Join(n.ruleCacheDir, name+"_verbalizer.fst")
		if _, err := os.Stat(taggerPath); err != nil {
			return false
		}
		if _, err := os.Stat(verbalizerPath); err != nil {
			return false
		}
	}
	// Also check extra intermediate FST files
	for _, name := range enRuleCacheExtraFiles {
		path := filepath.Join(n.ruleCacheDir, name+".fst")
		if _, err := os.Stat(path); err != nil {
			return false
		}
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
			n.cardinalRule = &rules.Cardinal{Processor: tn.NewProcessorLazy("cardinal", "en_tn")}
			n.cardinalRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "cardinal_tagger.fst"))
			n.cardinalRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "cardinal_verbalizer.fst"))
			graph, _ := pynini.FstRead(filepath.Join(n.ruleCacheDir, "cardinal_graph.fst"))
			graphHundred, _ := pynini.FstRead(filepath.Join(n.ruleCacheDir, "cardinal_graph_hundred.fst"))
			singleDigits, _ := pynini.FstRead(filepath.Join(n.ruleCacheDir, "cardinal_single_digits.fst"))
			longNumbers, _ := pynini.FstRead(filepath.Join(n.ruleCacheDir, "cardinal_long_numbers.fst"))
			n.cardinalRule.SetCachedGraph(graph)
			n.cardinalRule.SetCachedGraphHundredComponent(graphHundred)
			n.cardinalRule.SetCachedSingleDigits(singleDigits)
			n.cardinalRule.SetCachedLongNumbers(longNumbers)
		}},
		{"word", func() {
			n.wordRule = &rules.Word{Processor: tn.NewProcessorLazy("w", "en_tn")}
			n.wordRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "word_tagger.fst"))
			n.wordRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "word_verbalizer.fst"))
		}},
		{"punct", func() {
			n.punctRule = &rules.Punctuation{Processor: tn.NewProcessorLazy("p", "en_tn")}
			n.punctRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "punct_tagger.fst"))
			n.punctRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "punct_verbalizer.fst"))
			graph, _ := pynini.FstRead(filepath.Join(n.ruleCacheDir, "punct_graph.fst"))
			n.punctRule.SetCachedGraph(graph)
		}},
		{"ordinal", func() {
			n.ordinalRule = &rules.Ordinal{Processor: tn.NewProcessorLazy("ordinal", "en_tn")}
			n.ordinalRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "ordinal_tagger.fst"))
			n.ordinalRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "ordinal_verbalizer.fst"))
		}},
		{"decimal", func() {
			n.decimalRule = &rules.Decimal{Processor: tn.NewProcessorLazy("decimal", "en_tn")}
			n.decimalRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "decimal_tagger.fst"))
			n.decimalRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "decimal_verbalizer.fst"))
		}},
		{"fraction", func() {
			n.fractionRule = &rules.Fraction{Processor: tn.NewProcessorLazy("fraction", "en_tn")}
			n.fractionRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "fraction_tagger.fst"))
			n.fractionRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "fraction_verbalizer.fst"))
		}},
		{"date", func() {
			n.dateRule = &rules.Date{Processor: tn.NewProcessorLazy("date", "en_tn")}
			n.dateRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "date_tagger.fst"))
			n.dateRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "date_verbalizer.fst"))
		}},
		{"time", func() {
			n.timeRule = &rules.Time{Processor: tn.NewProcessorLazy("time", "en_tn")}
			n.timeRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "time_tagger.fst"))
			n.timeRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "time_verbalizer.fst"))
		}},
		{"money", func() {
			n.moneyRule = &rules.Money{Processor: tn.NewProcessorLazy("money", "en_tn")}
			n.moneyRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "money_tagger.fst"))
			n.moneyRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "money_verbalizer.fst"))
		}},
		{"telephone", func() {
			n.telephoneRule = &rules.Telephone{Processor: tn.NewProcessorLazy("telephone", "en_tn")}
			n.telephoneRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "telephone_tagger.fst"))
			n.telephoneRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "telephone_verbalizer.fst"))
		}},
		{"whitelist", func() {
			n.whitelistRule = &rules.Whitelist{Processor: tn.NewProcessorLazy("whitelist", "en_tn")}
			n.whitelistRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "whitelist_tagger.fst"))
			n.whitelistRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "whitelist_verbalizer.fst"))
		}},
		{"range", func() {
			n.rangeRule = &rules.Range{Processor: tn.NewProcessorLazy("range", "en_tn")}
			n.rangeRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "range_tagger.fst"))
			n.rangeRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "range_verbalizer.fst"))
		}},
		{"measure", func() {
			n.measureRule = &rules.Measure{Processor: tn.NewProcessorLazy("measure", "en_tn")}
			n.measureRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "measure_tagger.fst"))
			n.measureRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "measure_verbalizer.fst"))
		}},
		{"electronic", func() {
			n.electronicRule = &rules.Electronic{Processor: tn.NewProcessorLazy("electronic", "en_tn")}
			n.electronicRule.Tagger, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "electronic_tagger.fst"))
			n.electronicRule.Verbalizer, _ = pynini.FstRead(filepath.Join(n.ruleCacheDir, "electronic_verbalizer.fst"))
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
			n.Processor.ReportProgress("加载缓存-"+task.name, idx+1, len(tasks))
		}(t, i)
	}
	wg.Wait()
}

// buildRulesFromScratch builds all rule FSTs from scratch (the original logic).
func (n *Normalizer) buildRulesFromScratch() {
	concurrency, _ := n.Processor.GetBuildConfig()
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
			n.Processor.ReportProgress("构建Tagger-"+tt.name, idx+1, len(tasks))
		}(t, i)
	}
	wg.Wait()

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
		// NOTE: RmOutputEpsilon is NOT applied here because it creates
		// compound labels that span across different subgraphs (e.g., hundred_wa
		// and tens in cardinal), causing incorrect matching like "25" -> "two hundred five"
		// instead of "twenty five". The output-only epsilon arcs from Insert operations
		// are handled at runtime by the epsilon closure BFS.
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
	sortArcs(n.cardinalRule.Tagger)
	n.cardinalRule.Tagger = optimizeTagger(n.cardinalRule.Tagger)
	sortArcs(n.cardinalRule.Verbalizer)
	n.cardinalRule.Verbalizer = optimizeVerbalizer(n.cardinalRule.Verbalizer)
	n.Processor.ReportProgress("优化-cardinal", 1, 14)
	sortArcs(n.ordinalRule.Tagger)
	n.ordinalRule.Tagger = optimizeTagger(n.ordinalRule.Tagger)
	sortArcs(n.ordinalRule.Verbalizer)
	n.ordinalRule.Verbalizer = optimizeVerbalizer(n.ordinalRule.Verbalizer)
	n.Processor.ReportProgress("优化-ordinal", 2, 14)
	sortArcs(n.decimalRule.Tagger)
	n.decimalRule.Tagger = optimizeTagger(n.decimalRule.Tagger)
	sortArcs(n.decimalRule.Verbalizer)
	n.decimalRule.Verbalizer = optimizeVerbalizer(n.decimalRule.Verbalizer)
	n.Processor.ReportProgress("优化-decimal", 3, 14)
	sortArcs(n.fractionRule.Tagger)
	n.fractionRule.Tagger = optimizeTagger(n.fractionRule.Tagger)
	sortArcs(n.fractionRule.Verbalizer)
	n.fractionRule.Verbalizer = optimizeVerbalizer(n.fractionRule.Verbalizer)
	n.Processor.ReportProgress("优化-fraction", 4, 14)
	sortArcs(n.dateRule.Tagger)
	n.dateRule.Tagger = optimizeTagger(n.dateRule.Tagger)
	sortArcs(n.dateRule.Verbalizer)
	n.dateRule.Verbalizer = optimizeVerbalizer(n.dateRule.Verbalizer)
	n.Processor.ReportProgress("优化-date", 5, 14)
	sortArcs(n.timeRule.Tagger)
	n.timeRule.Tagger = optimizeTagger(n.timeRule.Tagger)
	sortArcs(n.timeRule.Verbalizer)
	n.timeRule.Verbalizer = optimizeVerbalizer(n.timeRule.Verbalizer)
	n.Processor.ReportProgress("优化-time", 6, 14)
	sortArcs(n.moneyRule.Tagger)
	n.moneyRule.Tagger = optimizeTagger(n.moneyRule.Tagger)
	sortArcs(n.moneyRule.Verbalizer)
	n.moneyRule.Verbalizer = optimizeVerbalizer(n.moneyRule.Verbalizer)
	n.Processor.ReportProgress("优化-money", 7, 14)
	sortArcs(n.telephoneRule.Tagger)
	n.telephoneRule.Tagger = optimizeTagger(n.telephoneRule.Tagger)
	sortArcs(n.telephoneRule.Verbalizer)
	n.telephoneRule.Verbalizer = optimizeVerbalizer(n.telephoneRule.Verbalizer)
	n.Processor.ReportProgress("优化-telephone", 8, 14)
	sortArcs(n.whitelistRule.Tagger)
	n.whitelistRule.Tagger = optimizeTagger(n.whitelistRule.Tagger)
	sortArcs(n.whitelistRule.Verbalizer)
	n.whitelistRule.Verbalizer = optimizeVerbalizer(n.whitelistRule.Verbalizer)
	n.Processor.ReportProgress("优化-whitelist", 9, 14)
	sortArcs(n.rangeRule.Tagger)
	n.rangeRule.Tagger = optimizeTagger(n.rangeRule.Tagger)
	sortArcs(n.rangeRule.Verbalizer)
	n.rangeRule.Verbalizer = optimizeVerbalizer(n.rangeRule.Verbalizer)
	n.Processor.ReportProgress("优化-range", 10, 14)
	sortArcs(n.wordRule.Tagger)
	n.wordRule.Tagger = optimizeTagger(n.wordRule.Tagger)
	sortArcs(n.wordRule.Verbalizer)
	n.wordRule.Verbalizer = optimizeVerbalizer(n.wordRule.Verbalizer)
	n.Processor.ReportProgress("优化-word", 11, 14)
	sortArcs(n.punctRule.Tagger)
	n.punctRule.Tagger = optimizeTagger(n.punctRule.Tagger)
	sortArcs(n.punctRule.Verbalizer)
	n.punctRule.Verbalizer = optimizeVerbalizer(n.punctRule.Verbalizer)
	n.Processor.ReportProgress("优化-punct", 12, 14)
	sortArcs(n.measureRule.Tagger)
	n.measureRule.Tagger = optimizeTagger(n.measureRule.Tagger)
	sortArcs(n.measureRule.Verbalizer)
	n.measureRule.Verbalizer = optimizeVerbalizer(n.measureRule.Verbalizer)
	n.Processor.ReportProgress("优化-measure", 13, 14)
	sortArcs(n.electronicRule.Tagger)
	n.electronicRule.Tagger = optimizeTagger(n.electronicRule.Tagger)
	sortArcs(n.electronicRule.Verbalizer)
	n.electronicRule.Verbalizer = optimizeVerbalizer(n.electronicRule.Verbalizer)
	n.Processor.ReportProgress("优化-electronic", 14, 14)
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
		{"cardinal_graph", n.cardinalRule.Graph},
		{"cardinal_graph_hundred", n.cardinalRule.GraphHundredComponentAtLeastOneNoneZeroDigit},
		{"cardinal_single_digits", n.cardinalRule.SingleDigitsGraph},
		{"cardinal_long_numbers", n.cardinalRule.LongNumbers},
		{"word_tagger", n.wordRule.Tagger},
		{"word_verbalizer", n.wordRule.Verbalizer},
		{"punct_tagger", n.punctRule.Tagger},
		{"punct_verbalizer", n.punctRule.Verbalizer},
		{"punct_graph", n.punctRule.Graph},
		{"ordinal_tagger", n.ordinalRule.Tagger},
		{"ordinal_verbalizer", n.ordinalRule.Verbalizer},
		{"decimal_tagger", n.decimalRule.Tagger},
		{"decimal_verbalizer", n.decimalRule.Verbalizer},
		{"fraction_tagger", n.fractionRule.Tagger},
		{"fraction_verbalizer", n.fractionRule.Verbalizer},
		{"date_tagger", n.dateRule.Tagger},
		{"date_verbalizer", n.dateRule.Verbalizer},
		{"time_tagger", n.timeRule.Tagger},
		{"time_verbalizer", n.timeRule.Verbalizer},
		{"money_tagger", n.moneyRule.Tagger},
		{"money_verbalizer", n.moneyRule.Verbalizer},
		{"telephone_tagger", n.telephoneRule.Tagger},
		{"telephone_verbalizer", n.telephoneRule.Verbalizer},
		{"whitelist_tagger", n.whitelistRule.Tagger},
		{"whitelist_verbalizer", n.whitelistRule.Verbalizer},
		{"range_tagger", n.rangeRule.Tagger},
		{"range_verbalizer", n.rangeRule.Verbalizer},
		{"measure_tagger", n.measureRule.Tagger},
		{"measure_verbalizer", n.measureRule.Verbalizer},
		{"electronic_tagger", n.electronicRule.Tagger},
		{"electronic_verbalizer", n.electronicRule.Verbalizer},
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

		// Fast-path: try quick pattern matching before expensive FST composition.
		// This handles common patterns (cardinal, decimal, date, time, money,
		// ordinal, measure, telephone, fraction, electronic) using Go code
		// instead of FST, which is 10-100x faster.
		if fp, consumed := n.tryFastPath(runes, pos); consumed > 0 {
			result.WriteString(fp)
			pos += consumed
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

// tryFastPath attempts to match common patterns using Go code instead of FST.
// Returns (verbalized_output, consumed_runes) if a fast-path match is found,
// or ("", 0) if no fast-path match (caller should fall back to FST).
func (n *Normalizer) tryFastPath(runes []rune, pos int) (string, int) {
	r := runes[pos]
	remaining := len(runes) - pos

	// Only digits and $ can trigger fast-path
	if r >= '0' && r <= '9' {
		// Try patterns in priority order (same as rule weights):
		// date > cardinal > ordinal > decimal > fraction > time > money > measure > telephone

		// 1. Date: YYYY-MM-DD, YYYY/MM/DD, YYYY.MM.DD
		if remaining >= 8 && r >= '1' && r <= '9' {
			if fp, consumed := n.tryFastPathDate(runes, pos, remaining); consumed > 0 {
				return fp, consumed
			}
		}

		// 2. Time: HH:MM or H:MM with optional am/pm
		if remaining >= 4 && (r >= '0' && r <= '9') {
			if fp, consumed := n.tryFastPathTime(runes, pos, remaining); consumed > 0 {
				return fp, consumed
			}
		}

		// 3. Telephone: NNN-NNN-NNNN-N or similar patterns
		if remaining >= 8 {
			if fp, consumed := n.tryFastPathTelephone(runes, pos, remaining); consumed > 0 {
				return fp, consumed
			}
		}

		// 4. Fraction: N/N
		if remaining >= 3 {
			if fp, consumed := n.tryFastPathFraction(runes, pos, remaining); consumed > 0 {
				return fp, consumed
			}
		}

		// 5. Decimal: N.N or .N (check before cardinal to handle "3.5" as decimal)
		if remaining >= 2 {
			if fp, consumed := n.tryFastPathDecimal(runes, pos, remaining); consumed > 0 {
				return fp, consumed
			}
		}

		// 6. Ordinal: Nth, Nst, Nnd, Nrd
		if remaining >= 3 {
			if fp, consumed := n.tryFastPathOrdinal(runes, pos, remaining); consumed > 0 {
				return fp, consumed
			}
		}

		// 7. Cardinal: plain digit sequence (fallback for digits)
		if fp, consumed := n.tryFastPathCardinal(runes, pos, remaining); consumed > 0 {
			return fp, consumed
		}

	} else if r == '$' {
		// Money: $N or $N.N
		if remaining >= 2 && runes[pos+1] >= '0' && runes[pos+1] <= '9' {
			if fp, consumed := n.tryFastPathMoney(runes, pos, remaining); consumed > 0 {
				return fp, consumed
			}
		}
	}

	return "", 0
}

// tryFastPathCardinal matches plain digit sequences as cardinal numbers.
// Only matches if the digit sequence is not part of a larger pattern
// (date, telephone, fraction, decimal, ordinal, money, measure).
func (n *Normalizer) tryFastPathCardinal(runes []rune, pos, remaining int) (string, int) {
	// Scan digits
	end := pos
	for end < len(runes) && runes[end] >= '0' && runes[end] <= '9' {
		end++
	}
	if end == pos {
		return "", 0
	}
	digitLen := end - pos

	// Skip if followed by colon (time), dot+digit (decimal), slash (fraction),
	// hyphen (date/telephone/range), or ordinal suffix
	if end < len(runes) {
		next := runes[end]
		if next == ':' || next == '.' || next == '/' || next == '-' {
			return "", 0
		}
		// Check ordinal suffix
		if digitLen <= 2 && end+1 < len(runes) {
			suffix := string(runes[end:end+1])
			if suffix == "s" || suffix == "n" || suffix == "r" {
				// Could be ordinal suffix (st, nd, rd)
				if end+2 <= len(runes) {
					suffix2 := string(runes[end : end+2])
					if suffix2 == "st" || suffix2 == "nd" || suffix2 == "rd" || suffix2 == "th" {
						return "", 0
					}
				}
			}
			if suffix == "t" && end+2 <= len(runes) {
				suffix2 := string(runes[end : end+2])
				if suffix2 == "th" {
					return "", 0
				}
			}
		}
	}
	// Skip if preceded by $ (money)
	if pos > 0 && runes[pos-1] == '$' {
		return "", 0
	}

	// Skip single digit "0" - let FST handle it (might be "oh" or "zero")
	digits := string(runes[pos:end])
	if digits == "0" {
		return "", 0
	}

	// Skip if followed by space + unit word (measure rule should handle it)
	// e.g., "10 km", "25 degrees", "3.5 kilometers"
	// Also skip if followed by space + month name (date rule should handle it)
	// e.g., "4 March", "21 March"
	if end < len(runes) && runes[end] == ' ' {
		unitEnd := end + 1
		for unitEnd < len(runes) && (runes[unitEnd] >= 'a' && runes[unitEnd] <= 'z' || runes[unitEnd] >= 'A' && runes[unitEnd] <= 'Z') {
			unitEnd++
		}
		unitWord := strings.ToLower(string(runes[end+1 : unitEnd]))
		if isMeasureUnit(unitWord) {
			return "", 0
		}
		if isMonthName(unitWord) {
			return "", 0
		}
	}

	// For 4-digit numbers that look like years (1000-2999), use year reading
	// e.g., "1988" -> "nineteen eighty eight" not "one thousand nine hundred and eighty eight"
	verbalized := ""
	if digitLen == 4 {
		firstDigit := digits[0]
		if firstDigit == '1' || firstDigit == '2' {
			verbalized = verbalizeYear(digits)
		}
	}
	if verbalized == "" {
		verbalized = verbalizeCardinalNumber(digits)
	}
	if verbalized == "" {
		return "", 0
	}

	// Check word boundary: if next char is alphanumeric, skip
	if end < len(runes) && isWordChar(runes[end]) {
		return "", 0
	}

	return verbalized, end - pos
}

// tryFastPathDecimal matches decimal numbers like "3.5", ".456", "31.990"
func (n *Normalizer) tryFastPathDecimal(runes []rune, pos, remaining int) (string, int) {
	// Find the decimal point
	dotPos := -1
	end := pos
	for end < len(runes) {
		if runes[end] == '.' {
			if dotPos >= 0 {
				break // second dot, stop
			}
			dotPos = end
		} else if runes[end] >= '0' && runes[end] <= '9' {
			// continue
		} else {
			break
		}
		end++
	}

	if dotPos < 0 || dotPos == pos && end-dotPos <= 1 {
		// No dot found, or just a dot without digits after it
		return "", 0
	}

	// Must have digits after the dot
	if dotPos+1 >= end {
		return "", 0
	}

	// Must have at least one digit (either before or after dot)
	if dotPos == pos && end-dotPos <= 1 {
		return "", 0
	}

	// Skip if preceded by $ (money)
	if pos > 0 && runes[pos-1] == '$' {
		return "", 0
	}

	// Skip if followed by alphanumeric (word boundary)
	if end < len(runes) && isWordChar(runes[end]) {
		return "", 0
	}

	// Check for quantity suffix: "billion", "million", "thousand"
	quantity := ""
	quantEnd := end
	if quantEnd < len(runes) && runes[quantEnd] == ' ' {
		// Look ahead for quantity word
		qStart := quantEnd + 1
		qEnd := qStart
		for qEnd < len(runes) && (runes[qEnd] >= 'a' && runes[qEnd] <= 'z' || runes[qEnd] >= 'A' && runes[qEnd] <= 'Z') {
			qEnd++
		}
		qWord := string(runes[qStart:qEnd])
		if qWord == "billion" || qWord == "million" || qWord == "thousand" || qWord == "trillion" {
			quantity = qWord
			quantEnd = qEnd
		} else if isMeasureUnit(strings.ToLower(qWord)) {
			// Measure unit after decimal - let measure rule handle it
			return "", 0
		}
	}

	intPart := ""
	if dotPos > pos {
		intPart = string(runes[pos:dotPos])
	}
	fracPart := string(runes[dotPos+1 : end])

	// Verbalize integer part
	intVerbalized := ""
	if intPart != "" && intPart != "0" {
		intVerbalized = verbalizeCardinalNumber(intPart)
	} else if intPart == "0" {
		intVerbalized = "zero"
	}

	// Verbalize fractional part digit by digit (with "oh" for "0")
	fracVerbalized := verbalizeFractionDigits(fracPart)

	// Build result
	result := ""
	if intVerbalized == "zero" {
		intVerbalized = "oh"
	}
	if intVerbalized != "" && fracVerbalized != "" {
		result = intVerbalized + " point " + fracVerbalized
	} else if fracVerbalized != "" {
		result = "point " + fracVerbalized
	} else if intVerbalized != "" {
		result = intVerbalized
	}

	if quantity != "" {
		result += " " + quantity
		end = quantEnd
	}

	if result == "" {
		return "", 0
	}

	// Return consumed length
	consumed := end - pos
	if quantity != "" {
		consumed = quantEnd - pos
	}
	return result, consumed
}

// tryFastPathDate matches date patterns: YYYY-MM-DD, YYYY/MM/DD, YYYY.MM.DD
func (n *Normalizer) tryFastPathDate(runes []rune, pos, remaining int) (string, int) {
	// Must start with 4 digits (year)
	if remaining < 8 {
		return "", 0
	}
	// Check year: 4 digits
	for i := 0; i < 4; i++ {
		if runes[pos+i] < '0' || runes[pos+i] > '9' {
			return "", 0
		}
	}
	// Check separator
	sep := runes[pos+4]
	if sep != '-' && sep != '/' && sep != '.' {
		return "", 0
	}
	// Check month: 2 digits (01-12)
	if runes[pos+5] < '0' || runes[pos+5] > '9' || runes[pos+6] < '0' || runes[pos+6] > '9' {
		return "", 0
	}
	month := int(runes[pos+5]-'0')*10 + int(runes[pos+6]-'0')
	if month < 1 || month > 12 {
		return "", 0
	}
	// Check second separator
	if runes[pos+7] != sep {
		return "", 0
	}
	// Check day: 2 digits (01-31)
	if remaining < 10 || runes[pos+8] < '0' || runes[pos+8] > '9' || runes[pos+9] < '0' || runes[pos+9] > '9' {
		return "", 0
	}
	day := int(runes[pos+8]-'0')*10 + int(runes[pos+9]-'0')
	if day < 1 || day > 31 {
		return "", 0
	}

	// Skip if followed by alphanumeric (word boundary)
	end := pos + 10
	if end < len(runes) && isWordChar(runes[end]) {
		return "", 0
	}

	// Verbalize date: "the Nth of Month Year"
	yearStr := string(runes[pos : pos+4])
	yearVerbalized := verbalizeYear(yearStr)
	monthVerbalized := monthNames[month]
	dayVerbalized := verbalizeDay(day)

	return "the " + dayVerbalized + " of " + monthVerbalized + " " + yearVerbalized, 10
}

// tryFastPathTime matches time patterns: HH:MM with optional am/pm/a.m./p.m.
func (n *Normalizer) tryFastPathTime(runes []rune, pos, remaining int) (string, int) {
	// Find colon
	colonPos := -1
	end := pos
	for end < len(runes) && end-pos < 6 {
		if runes[end] == ':' {
			colonPos = end
			break
		}
		if runes[end] < '0' || runes[end] > '9' {
			return "", 0
		}
		end++
	}
	if colonPos < 0 || colonPos == pos {
		return "", 0
	}

	// Hour: 1-2 digits
	hourStr := string(runes[pos:colonPos])
	hour := 0
	for _, c := range hourStr {
		hour = hour*10 + int(c-'0')
	}
	if hour < 0 || hour > 23 {
		return "", 0
	}

	// Minute: 2 digits
	if colonPos+2 >= len(runes) {
		return "", 0
	}
	minuteStr := string(runes[colonPos+1 : colonPos+3])
	minute := 0
	for _, c := range minuteStr {
		if c < '0' || c > '9' {
			return "", 0
		}
		minute = minute*10 + int(c-'0')
	}
	if minute < 0 || minute > 59 {
		return "", 0
	}

	end = colonPos + 3

	// Check for am/pm suffix (try a.m./p.m. first since it's longer)
	ampm := ""
	if end < len(runes) {
		// Skip space
		tpos := end
		if tpos < len(runes) && runes[tpos] == ' ' {
			tpos++
		}
		// Check for a.m./p.m. (with dots) - 4 chars after the letter
		if tpos+4 <= len(runes) &&
			(runes[tpos] == 'a' || runes[tpos] == 'A' || runes[tpos] == 'p' || runes[tpos] == 'P') &&
			runes[tpos+1] == '.' && (runes[tpos+2] == 'm' || runes[tpos+2] == 'M') && runes[tpos+3] == '.' {
			ch := strings.ToLower(string(runes[tpos]))
			if ch == "a" {
				ampm = "AM"
			} else {
				ampm = "PM"
			}
			end = tpos + 4
		} else if tpos+2 <= len(runes) {
			// Check for am/pm (without dots)
			suffix := strings.ToLower(string(runes[tpos : tpos+2]))
			if suffix == "am" {
				ampm = "AM"
				end = tpos + 2
			} else if suffix == "pm" {
				ampm = "PM"
				end = tpos + 2
			}
		}
	}

	// Skip if followed by alphanumeric (word boundary)
	if end < len(runes) && isWordChar(runes[end]) {
		return "", 0
	}

	// Verbalize time
	var result string
	if minute == 0 {
		result = verbalizeCardinalNumber(fmt.Sprintf("%d", hour)) + " o'clock"
	} else if minute < 10 {
		result = verbalizeCardinalNumber(fmt.Sprintf("%d", hour)) + " oh " + verbalizeCardinalNumber(fmt.Sprintf("%d", minute))
	} else {
		result = verbalizeCardinalNumber(fmt.Sprintf("%d", hour)) + " " + verbalizeCardinalNumber(fmt.Sprintf("%d", minute))
	}
	if ampm != "" {
		result += " " + ampm
	}

	return result, end - pos
}

// tryFastPathMoney matches money patterns: $N, $N.N, $N million, etc.
func (n *Normalizer) tryFastPathMoney(runes []rune, pos, remaining int) (string, int) {
	if runes[pos] != '$' {
		return "", 0
	}
	// Scan digits and optional decimal point after $
	dpos := pos + 1
	dotPos := -1
	for dpos < len(runes) {
		if runes[dpos] >= '0' && runes[dpos] <= '9' {
			dpos++
		} else if runes[dpos] == '.' && dotPos < 0 {
			dotPos = dpos
			dpos++
		} else {
			break
		}
	}
	if dpos == pos+1 {
		return "", 0 // no digits after $
	}

	// Check for quantity suffix
	quantity := ""
	quantEnd := dpos
	if quantEnd < len(runes) && runes[quantEnd] == ' ' {
		qStart := quantEnd + 1
		qEnd := qStart
		for qEnd < len(runes) && (runes[qEnd] >= 'a' && runes[qEnd] <= 'z' || runes[qEnd] >= 'A' && runes[qEnd] <= 'Z') {
			qEnd++
		}
		qWord := string(runes[qStart:qEnd])
		if qWord == "billion" || qWord == "million" || qWord == "thousand" || qWord == "trillion" {
			quantity = qWord
			quantEnd = qEnd
		}
	}

	// Skip if followed by alphanumeric (word boundary)
	if quantEnd < len(runes) && isWordChar(runes[quantEnd]) {
		return "", 0
	}

	// Verbalize
	intPart := ""
	fracPart := ""
	if dotPos >= 0 {
		intPart = string(runes[pos+1 : dotPos])
		fracPart = string(runes[dotPos+1 : dpos])
	} else {
		intPart = string(runes[pos+1 : dpos])
	}

	intVerbalized := verbalizeCardinalNumber(intPart)
	if intVerbalized == "" {
		return "", 0
	}

	result := intVerbalized
	if fracPart != "" {
		fracVerbalized := verbalizeFractionDigits(fracPart)
		fracVerbalized = stripTrailingZeros(fracVerbalized)
		if fracVerbalized != "" {
			result += " point " + fracVerbalized
		}
	}
	if quantity != "" {
		result += " " + quantity
	}
	result += " dollars"

	consumed := quantEnd - pos
	if quantity == "" {
		consumed = dpos - pos
	}
	return result, consumed
}

// tryFastPathOrdinal matches ordinal patterns: 1st, 2nd, 3rd, 4th, etc.
func (n *Normalizer) tryFastPathOrdinal(runes []rune, pos, remaining int) (string, int) {
	// Scan digits
	end := pos
	for end < len(runes) && runes[end] >= '0' && runes[end] <= '9' {
		end++
	}
	if end == pos || end+1 >= len(runes) {
		return "", 0
	}
	// Check ordinal suffix
	suffix := string(runes[end:])
	ordinalSuffix := ""
	if strings.HasPrefix(suffix, "st") {
		ordinalSuffix = "st"
	} else if strings.HasPrefix(suffix, "nd") {
		ordinalSuffix = "nd"
	} else if strings.HasPrefix(suffix, "rd") {
		ordinalSuffix = "rd"
	} else if strings.HasPrefix(suffix, "th") {
		ordinalSuffix = "th"
	}
	if ordinalSuffix == "" {
		return "", 0
	}

	// Skip if followed by alphanumeric (word boundary)
	suffixEnd := end + len(ordinalSuffix)
	if suffixEnd < len(runes) && isWordChar(runes[suffixEnd]) {
		return "", 0
	}

	digits := string(runes[pos:end])
	verbalized := verbalizeOrdinalNumber(digits)
	if verbalized == "" {
		return "", 0
	}

	return verbalized, suffixEnd - pos
}

// tryFastPathTelephone matches telephone patterns: NNN-NNN-NNNN-N
func (n *Normalizer) tryFastPathTelephone(runes []rune, pos, remaining int) (string, int) {
	// Look for pattern: digits and hyphens
	end := pos
	digitCount := 0
	hyphenCount := 0
	for end < len(runes) {
		if runes[end] >= '0' && runes[end] <= '9' {
			digitCount++
			end++
		} else if runes[end] == '-' {
			hyphenCount++
			end++
		} else {
			break
		}
	}
	// Must have at least 8 digits and 2+ hyphens
	if digitCount < 8 || hyphenCount < 2 {
		return "", 0
	}

	// Skip if followed by alphanumeric (word boundary)
	if end < len(runes) && isWordChar(runes[end]) {
		return "", 0
	}

	// Verbalize: read each digit individually, hyphens become commas
	var result strings.Builder
	for i := pos; i < end; i++ {
		if runes[i] >= '0' && runes[i] <= '9' {
			if result.Len() > 0 {
				result.WriteString(" ")
			}
			result.WriteString(digitNames[runes[i]-'0'])
		} else if runes[i] == '-' {
			result.WriteString(",")
		}
	}

	return result.String(), end - pos
}

// tryFastPathFraction matches fraction patterns: N/N
func (n *Normalizer) tryFastPathFraction(runes []rune, pos, remaining int) (string, int) {
	// Scan numerator digits
	end := pos
	for end < len(runes) && runes[end] >= '0' && runes[end] <= '9' {
		end++
	}
	if end == pos || end >= len(runes) || runes[end] != '/' {
		return "", 0
	}
	slashPos := end
	// Scan denominator digits
	end++
	denomStart := end
	for end < len(runes) && runes[end] >= '0' && runes[end] <= '9' {
		end++
	}
	if end == denomStart {
		return "", 0
	}

	// Skip if followed by alphanumeric (word boundary)
	if end < len(runes) && isWordChar(runes[end]) {
		return "", 0
	}

	numerator := string(runes[pos:slashPos])
	denominator := string(runes[slashPos+1 : end])

	numVal := verbalizeCardinalNumber(numerator)
	denomVal := verbalizeCardinalNumber(denominator)
	if numVal == "" || denomVal == "" {
		return "", 0
	}

	// Use fraction names: 1/2 -> one half, 1/3 -> one third, 1/4 -> one quarter, etc.
	num := 0
	for _, c := range numerator {
		num = num*10 + int(c-'0')
	}
	den := 0
	for _, c := range denominator {
		den = den*10 + int(c-'0')
	}

	fracName := fractionName(num, den)
	if fracName != "" {
		return fracName, end - pos
	}

	return numVal + " over " + denomVal, end - pos
}

// verbalizeCardinalNumber converts a digit string to its English cardinal form.
// e.g., "123" -> "one hundred and twenty three"
func verbalizeCardinalNumber(digits string) string {
	if len(digits) == 0 {
		return ""
	}

	// Handle negative
	negative := false
	if digits[0] == '-' {
		negative = true
		digits = digits[1:]
	}

	if len(digits) == 0 {
		return ""
	}

	// Remove leading zeros for numbers > 1 digit
	if len(digits) > 1 {
		i := 0
		for i < len(digits)-1 && digits[i] == '0' {
			i++
		}
		digits = digits[i:]
	}

	result := verbalizeCardinalDigits(digits)

	if negative && result != "" {
		result = "negative " + result
	}

	return result
}

// verbalizeCardinalDigits converts a digit string (no leading zeros except "0") to English.
func verbalizeCardinalDigits(digits string) string {
	n := len(digits)
	if n == 0 {
		return ""
	}

	if n == 1 {
		return digitNames[digits[0]-'0']
	}

	if n == 2 {
		return verbalizeTwoDigits(digits)
	}

	if n == 3 {
		hundreds := verbalizeCardinalDigits(digits[:1]) + " hundred"
		remainder := digits[1:]
		if remainder == "00" {
			return hundreds
		}
		remVerbalized := verbalizeTwoDigits(remainder)
		if remVerbalized == "" {
			return hundreds
		}
		return hundreds + " and " + remVerbalized
	}

	// For 4+ digits, use thousand/million/billion grouping
	if n <= 6 {
		thousands := verbalizeCardinalDigits(digits[:n-3])
		remainder := digits[n-3:]
		if remainder == "000" {
			return thousands + " thousand"
		}
		remVerbalized := verbalizeCardinalDigits(remainder)
		if remVerbalized == "" {
			return thousands + " thousand"
		}
		// Add "and" for thousand-level remainder < 100
		remVal := 0
		for _, c := range remainder {
			remVal = remVal*10 + int(c-'0')
		}
		if remVal > 0 && remVal < 100 {
			return thousands + " thousand and " + remVerbalized
		}
		return thousands + " thousand " + remVerbalized
	}

	if n <= 9 {
		millions := verbalizeCardinalDigits(digits[:n-6])
		remainder := digits[n-6:]
		if remainder == "000000" {
			return millions + " million"
		}
		remVerbalized := verbalizeCardinalDigits(remainder)
		if remVerbalized == "" {
			return millions + " million"
		}
		return millions + " million " + remVerbalized
	}

	if n <= 12 {
		billions := verbalizeCardinalDigits(digits[:n-9])
		remainder := digits[n-9:]
		if remainder == "000000000" {
			return billions + " billion"
		}
		remVerbalized := verbalizeCardinalDigits(remainder)
		if remVerbalized == "" {
			return billions + " billion"
		}
		return billions + " billion " + remVerbalized
	}

	// Too large, fall back to digit-by-digit
	return verbalizeFractionDigits(digits)
}

// verbalizeTwoDigits converts a 2-digit string to English.
func verbalizeTwoDigits(digits string) string {
	if len(digits) != 2 {
		return ""
	}
	tens := digits[0] - '0'
	ones := digits[1] - '0'

	if tens == 0 {
		if ones == 0 {
			return ""
		}
		return digitNames[ones]
	}

	if tens == 1 {
		// Teens
		return teenNames[ones]
	}

	result := tyNames[tens]
	if ones > 0 {
		result += " " + digitNames[ones]
	}
	return result
}

// verbalizeFractionDigits verbalizes each digit individually (for decimal fractional parts).
// "0" -> "oh", "1" -> "one", etc.
func verbalizeFractionDigits(digits string) string {
	if len(digits) == 0 {
		return ""
	}
	var result strings.Builder
	for i, c := range digits {
		if i > 0 {
			result.WriteByte(' ')
		}
		if c == '0' {
			result.WriteString("oh")
		} else {
			result.WriteString(digitNames[c-'0'])
		}
	}
	return result.String()
}

// verbalizeYear converts a 4-digit year string to English.
// "2024" -> "twenty twenty four", "1988" -> "nineteen eighty eight"
func verbalizeYear(year string) string {
	if len(year) != 4 {
		return verbalizeCardinalNumber(year)
	}
	// Split into two 2-digit parts
	first := verbalizeTwoDigits(year[:2])
	second := verbalizeTwoDigits(year[2:])
	if second == "" {
		return first
	}
	return first + " " + second
}

// verbalizeDay converts a day number to its ordinal English form.
// 1 -> "first", 2 -> "second", 3 -> "third", etc.
func verbalizeDay(day int) string {
	if day >= 1 && day <= 31 {
		return ordinalDayNames[day]
	}
	return verbalizeCardinalNumber(fmt.Sprintf("%d", day))
}

// verbalizeOrdinalNumber converts a digit string to its ordinal English form.
// "1" -> "first", "12" -> "twelfth", "21" -> "twenty first"
func verbalizeOrdinalNumber(digits string) string {
	n := len(digits)
	if n == 0 {
		return ""
	}

	// Special case: 11, 12, 13 always use "th" (except 12 -> twelfth)
	if n >= 2 {
		lastTwo := digits[n-2:]
		if lastTwo == "11" {
			return verbalizeCardinalNumber(digits) + "th"
		}
		if lastTwo == "12" {
			return "twelfth"
		}
		if lastTwo == "13" {
			return verbalizeCardinalNumber(digits) + "th"
		}
	}

	lastDigit := digits[n-1]
	cardinalBase := verbalizeCardinalNumber(digits)

	switch lastDigit {
	case '1':
		// Special: "one" -> "first"
		if cardinalBase == "one" {
			return "first"
		}
		return strings.TrimSuffix(cardinalBase, "one") + "first"
	case '2':
		// Special: "two" -> "second", "twelve" -> "twelfth"
		if cardinalBase == "twelve" {
			return "twelfth"
		}
		if cardinalBase == "two" {
			return "second"
		}
		return strings.TrimSuffix(cardinalBase, "two") + "second"
	case '3':
		// Special: "three" -> "third"
		if cardinalBase == "three" {
			return "third"
		}
		return strings.TrimSuffix(cardinalBase, "three") + "third"
	default:
		return cardinalBase + "th"
	}
}

// fractionName returns the English name for common fractions.
func fractionName(num, den int) string {
	// Special fractions
	if num == 1 {
		switch den {
		case 2:
			return "one half"
		case 3:
			return "one third"
		case 4:
			return "one quarter"
		case 5:
			return "one fifth"
		case 6:
			return "one sixth"
		case 7:
			return "one seventh"
		case 8:
			return "one eighth"
		case 9:
			return "one ninth"
		case 10:
			return "one tenth"
		}
	} else if num == 3 && den == 4 {
		return "three quarters"
	} else if num == 2 && den == 3 {
		return "two thirds"
	}
	return ""
}

// English number word tables
var digitNames = [10]string{
	"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine",
}

var teenNames = [10]string{
	"ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen",
	"seventeen", "eighteen", "nineteen",
}

var tyNames = [10]string{
	"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety",
}

var monthNames = [13]string{
	"", "january", "february", "march", "april", "may", "june",
	"july", "august", "september", "october", "november", "december",
}

var ordinalDayNames = [32]string{
	"",
	"first", "second", "third", "fourth", "fifth", "sixth", "seventh",
	"eighth", "ninth", "tenth", "eleventh", "twelfth", "thirteenth",
	"fourteenth", "fifteenth", "sixteenth", "seventeenth", "eighteenth",
	"nineteenth", "twentieth", "twenty first", "twenty second",
	"twenty third", "twenty fourth", "twenty fifth", "twenty sixth",
	"twenty seventh", "twenty eighth", "twenty ninth", "thirtieth",
	"thirty first",
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

// computeMinMatchLen computes the minimum number of non-epsilon input symbols
// required to reach a final state from the start state of the given FST.
// This is used to quickly skip rules that require more input than available.
// Uses BFS to find the shortest path (by non-epsilon arc count) to any final state.
func computeMinMatchLen(fst *pynini.Fst) int {
	if fst == nil || len(fst.States) == 0 {
		return 0
	}
	// BFS with (state, depth) pairs. Depth = number of non-epsilon input arcs taken.
	type entry struct {
		state int32
		depth int
	}
	visited := make([]bool, len(fst.States))
	queue := []entry{{fst.Start, 0}}
	visited[fst.Start] = true

	for len(queue) > 0 {
		e := queue[0]
		queue = queue[1:]
		st := &fst.States[e.state]
		if st.Final && e.depth > 0 {
			return e.depth
		}
		for i := range st.Arcs {
			arc := &st.Arcs[i]
			nextState := arc.Next
			if visited[nextState] {
				continue
			}
			visited[nextState] = true
			if arc.ILabel == pynini.EpsilonLabel {
				// Epsilon arc: don't increment depth
				queue = append(queue, entry{nextState, e.depth})
			} else {
				// Non-epsilon arc: increment depth
				queue = append(queue, entry{nextState, e.depth + 1})
			}
		}
	}
	return 0
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

		// Fast-path: skip rules that require more input than remaining.
		if r.minMatchLen > 0 && remaining < r.minMatchLen {
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
// Field names match the tagger: "integer_part", "fractional_part", "quantity", "negative".
// In non-deterministic mode, Python uses "oh" for trailing zeros in the fractional part.
func verbalizeDecimalFast(reordered string) string {
	intVal := extractFieldValue(reordered, "integer_part")
	fracVal := extractFieldValue(reordered, "fractional_part")
	quantVal := extractFieldValue(reordered, "quantity")
	if intVal == "" && fracVal == "" {
		return ""
	}
	// Replace "zero" with "oh" in fractional part to match Python behavior.
	// Python's non-deterministic mode uses single_digits_graph_oh which
	// maps "0" -> "oh", and the test data expects "oh" for trailing zeros.
	fracVal = strings.ReplaceAll(fracVal, "zero", "oh")
	// Also replace integer_part "zero" with "oh" for cases like ".456"
	if intVal == "zero" {
		intVal = "oh"
	}
	result := intVal
	if fracVal != "" {
		if intVal != "" {
			result += " point " + fracVal
		} else {
			result = "point " + fracVal
		}
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

// isMeasureUnit checks if a word is a common measurement unit.
// These should be handled by the measure rule, not the cardinal fast-path.
func isMeasureUnit(word string) bool {
	switch word {
	case "km", "m", "cm", "mm", "kg", "g", "mg", "lb", "lbs", "oz",
		"miles", "mile", "feet", "foot", "ft", "in", "inch", "inches",
		"yards", "yard", "yd", "meters", "meter", "kilometers", "kilometer",
		"centimeters", "centimeter", "millimeters", "millimeter",
		"kilograms", "kilogram", "grams", "gram", "milligrams", "milligram",
		"pounds", "pound", "ounces", "ounce",
		"liters", "liter", "litres", "litre", "ml", "l",
		"degrees", "degree", "celsius", "fahrenheit",
		"mph", "kph", "fps",
		"hectares", "hectare", "acres", "acre",
		"tons", "ton", "tonnes", "tonne",
		"watts", "watt", "kilowatts", "kilowatt",
		"volts", "volt", "amperes", "ampere", "amps", "amp",
		"joules", "joule", "calories", "calorie", "kcal",
		"hertz", "hz", "khz", "mhz", "ghz",
		"percent", "percents":
		return true
	}
	return false
}

// isMonthName checks if a word is an English month name.
func isMonthName(word string) bool {
	switch word {
	case "january", "february", "march", "april", "may", "june",
		"july", "august", "september", "october", "november", "december":
		return true
	}
	return false
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

// DebugMatchResult holds debug info for a single rule match attempt.
type DebugMatchResult struct {
	RuleName  string
	Consumed  int
	Output    string
	Weight    float32
	Skipped   bool
	SkipReason string
}

// DebugMatch tries each rule at the given position and returns detailed results.
func (n *Normalizer) DebugMatch(input string, pos int) []DebugMatchResult {
	runes := []rune(input)
	var results []DebugMatchResult

	for _, r := range n.rules {
		dr := DebugMatchResult{RuleName: r.name}

		if r.tagger == nil || len(r.tagger.States) == 0 {
			dr.Skipped = true
			dr.SkipReason = "no tagger"
			results = append(results, dr)
			continue
		}

		if len(r.triggerSet) > 0 && !r.triggerSet[runes[pos]] {
			dr.Skipped = true
			dr.SkipReason = "triggerSet miss"
			results = append(results, dr)
			continue
		}

		maxLen := 30
		remaining := len(runes) - pos
		if remaining < maxLen {
			maxLen = remaining
		}

		result := pynini.ComposePrefixShortestPathRunes(runes, pos, maxLen, r.tagger)
		dr.Consumed = result.Consumed
		dr.Output = result.Output
		dr.Weight = r.weight + result.Weight

		if result.Consumed == 0 || result.Output == "" {
			dr.Skipped = true
			dr.SkipReason = "no match"
		} else if !strings.Contains(result.Output, r.name) {
			dr.Skipped = true
			dr.SkipReason = "rule name not in output"
		}

		results = append(results, dr)
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
