package tn

import (
	"os"
	"path/filepath"
	"time"

	"github.com/TelenLiu/WeTextProcessing-go/fstcache"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
)

type Processor struct {
	ALPHA             *pynini.Fst
	DIGIT             *pynini.Fst
	PUNCT             *pynini.Fst
	SPACE             *pynini.Fst
	VCHAR             *pynini.Fst
	VSIGMA            *pynini.Fst
	LOWER             *pynini.Fst
	UPPER             *pynini.Fst
	CHAR              *pynini.Fst
	SIGMA             *pynini.Fst
	NOT_QUOTE         *pynini.Fst
	NOT_SPACE         *pynini.Fst
	INSERT_SPACE      *pynini.Fst
	DELETE_SPACE      *pynini.Fst
	DELETE_EXTRA_SPACE *pynini.Fst
	DELETE_ZERO_OR_ONE_SPACE *pynini.Fst
	MIN_NEG_WEIGHT    float64
	TO_LOWER          *pynini.Fst
	TO_UPPER          *pynini.Fst

	Name              string
	Ordertype         string
	Tagger            *pynini.Fst
	Verbalizer        *pynini.Fst

	// FST cache manager (lazy load + TTL eviction)
	cache               *fstcache.Manager
	cachePrefix         string
	taggerBuilder       func() *pynini.Fst
	verbalizerBuilder   func() *pynini.Fst
	taggerRef           *pynini.Fst // cached tagger FST reference for fast access
	verbalizerRef       *pynini.Fst // cached verbalizer FST reference for fast access

	// Build configuration
	buildConcurrency int
	buildProgress    BuildProgressFn

	// Lazy base FST initialization
	baseFstLoaded bool

	// Cached TokenParser (reused across Verbalize calls)
	tokenParser *TokenParser
}

func (p *Processor) GetBuildConfig() (concurrency int, progress BuildProgressFn) {
	return p.buildConcurrency, p.buildProgress
}

func NewProcessor(name string, ordertype ...string) *Processor {
	ot := "tn"
	if len(ordertype) > 0 {
		ot = ordertype[0]
	}

	// Use CJK-extended VCHAR if available (for Chinese/Japanese processing)
	vchar := lib.VALID_UTF8_CHAR
	if cjk := lib.CJKVCHAR(); cjk != nil {
		vchar = cjk
	}

	p := &Processor{
		Name:       name,
		Ordertype:  ot,
		ALPHA:      lib.ALPHA,
		DIGIT:      lib.DIGIT,
		PUNCT:      lib.PUNCT,
		SPACE:      pynini.Union(lib.SPACE, pynini.Accep("\u00A0")),
		VCHAR:      vchar,
		LOWER:      lib.LOWER,
		UPPER:      lib.UPPER,
		MIN_NEG_WEIGHT: -0.0001,
	}
	// Build base FSTs by default. Callers that want to load from cache
	// should use NewProcessorLazy instead.
	p.buildBaseFst()
	p.tokenParser = NewTokenParser(ot)
	return p
}

// NewProcessorLazy creates a Processor without building base FSTs.
// The caller must call InitBaseFstCache or buildBaseFst before using
// any method that requires base FSTs (BuildTagger, BuildVerbalizer, etc.).
func NewProcessorLazy(name string, ordertype ...string) *Processor {
	ot := "tn"
	if len(ordertype) > 0 {
		ot = ordertype[0]
	}

	vchar := lib.VALID_UTF8_CHAR
	if cjk := lib.CJKVCHAR(); cjk != nil {
		vchar = cjk
	}

	p := &Processor{
		Name:       name,
		Ordertype:  ot,
		ALPHA:      lib.ALPHA,
		DIGIT:      lib.DIGIT,
		PUNCT:      lib.PUNCT,
		SPACE:      pynini.Union(lib.SPACE, pynini.Accep("\u00A0")),
		VCHAR:      vchar,
		LOWER:      lib.LOWER,
		UPPER:      lib.UPPER,
		MIN_NEG_WEIGHT: -0.0001,
	}
	p.tokenParser = NewTokenParser(ot)
	return p
}

// buildBaseFst constructs all base FSTs from scratch.
func (p *Processor) buildBaseFst() {
	if p.baseFstLoaded {
		return
	}
	p.baseFstLoaded = true
	p.VSIGMA = p.VCHAR.Star()
	CHAR := p.VCHAR.Difference(pynini.Union(pynini.Accep("\\"), pynini.Accep("\"")))
	p.CHAR = pynini.Union(
		CHAR,
		pynini.Cross("\\", "\\\\"),
		pynini.Cross("\"", "\\\""),
	)
	sigmaBase := pynini.Union(
		CHAR,
		pynini.Cross("\\\\", "\\"),
		pynini.Cross("\\\"", "\""),
	)
	p.SIGMA = sigmaBase.Star()
	p.NOT_QUOTE = p.VCHAR.Difference(pynini.Accep("\""))
	p.NOT_SPACE = p.VCHAR.Difference(p.SPACE)

	lower := "abcdefghijklmnopqrstuvwxyz"
	upper := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	var fsts []*pynini.Fst
	for i := 0; i < len(lower); i++ {
		fsts = append(fsts, pynini.Cross(string(upper[i]), string(lower[i])))
	}
	p.TO_LOWER = pynini.Union(fsts...)
	p.TO_UPPER = p.TO_LOWER.Invert()

	p.INSERT_SPACE = lib.Insert(" ")
	p.DELETE_SPACE = lib.Delete(p.SPACE).Star()
	p.DELETE_EXTRA_SPACE = pynini.Cross(p.SPACE.Plus(), pynini.Accep(" "))
	p.DELETE_ZERO_OR_ONE_SPACE = lib.Delete(p.SPACE).Ques()
}

func (p *Processor) BuildRule(fst *pynini.Fst, l, r string) *pynini.Fst {
	if l == "" && r == "" {
		sigmaStar := pynini.Cdrewrite(fst, "", "", p.VSIGMA)
		return sigmaStar
	}
	return pynini.Cdrewrite(fst, l, r, p.VSIGMA)
}

func (p *Processor) AddTokens(tagger *pynini.Fst) *pynini.Fst {
	tagger = lib.Insert(p.Name + " { ").Concat(tagger).Concat(lib.Insert(" } "))
	return tagger
}

func (p *Processor) DeleteTokens(verbalizer *pynini.Fst) *pynini.Fst {
	verbalizer = lib.DeleteString(p.Name).Concat(lib.DeleteString(" { ")).Concat(verbalizer).Concat(lib.DeleteString(" }")).Concat(lib.DeleteString(" ").Ques())
	return verbalizer
}

func (p *Processor) BuildVerbalizer() {
	verbalizer := lib.DeleteString("value: \"").Concat(p.SIGMA).Concat(lib.DeleteString("\""))
	p.Verbalizer = p.DeleteTokens(verbalizer)
}

// BuildProgressFn reports build progress: stage name, current step, total steps.
type BuildProgressFn func(stage string, current, total int)

// InitBaseFstCache loads or builds base FSTs only (no monolithic tagger/verbalizer).
// Used by per-rule normalizers that don't need monolithic FSTs.
func (p *Processor) InitBaseFstCache(prefix, cacheDir string, overwriteCache bool, progress BuildProgressFn) {
	if cacheDir == "" {
		cacheDir = "cache"
	}
	p.buildConcurrency = 2
	p.buildProgress = progress
	os.MkdirAll(cacheDir, 0755)

	// Step 1: Load or build base FSTs (VSIGMA, CHAR, SIGMA, etc.)
	if !p.tryLoadBaseFst(cacheDir, prefix) {
		if progress != nil {
			progress("构建基座FST", 1, 2)
		}
		p.buildBaseFst()
		p.saveBaseFst(cacheDir, prefix)
	}

	// Initialize cache manager for potential future use
	p.cache = fstcache.New(cacheDir)
	p.cache.StartEviction()
	p.cachePrefix = prefix

	if progress != nil {
		progress("完成", 2, 2)
	}
}

// BuildFstWithCache handles cache read/write for tagger and verbalizer using
// the FST cache manager (lazy load + TTL eviction). To skip the cache and
// force a rebuild, set overwriteCache=true or set ttl=0.
// concurrency controls how many rules are built in parallel (default 2).
// progress is an optional callback for build progress reporting.
func (p *Processor) BuildFstWithCache(prefix, cacheDir string, overwriteCache bool, concurrency int, progress BuildProgressFn, buildTagger, buildVerbalizer func()) {
	if cacheDir == "" {
		cacheDir = "cache"
	}
	if concurrency < 1 {
		concurrency = 2
	}
	p.buildConcurrency = concurrency
	p.buildProgress = progress
	os.MkdirAll(cacheDir, 0755)

	// Step 1: Load or build base FSTs (VSIGMA, CHAR, SIGMA, etc.)
	if !p.tryLoadBaseFst(cacheDir, prefix) {
		if progress != nil {
			progress("构建基座FST", 1, 3)
		}
		p.buildBaseFst()
		p.saveBaseFst(cacheDir, prefix)
	}

	// Initialize cache manager (10 min TTL eviction)
	p.cache = fstcache.New(cacheDir)
	p.cache.StartEviction()
	p.cachePrefix = prefix

	// Store builder functions for lazy reload if evicted
	p.taggerBuilder = func() *pynini.Fst {
		buildTagger()
		return p.Tagger
	}
	p.verbalizerBuilder = func() *pynini.Fst {
		buildVerbalizer()
		return p.Verbalizer
	}

	if overwriteCache {
		p.cache.Invalidate(prefix + "_tagger")
		p.cache.Invalidate(prefix + "_verbalizer")
	}

	if progress != nil {
		progress("加载Tagger", 2, 3)
	}
	p.Tagger = p.cache.GetOrBuild(prefix+"_tagger", p.taggerBuilder)
	if progress != nil {
		progress("加载Verbalizer", 3, 3)
	}
	p.Verbalizer = p.cache.GetOrBuild(prefix+"_verbalizer", p.verbalizerBuilder)
	if progress != nil {
		progress("完成", 3, 3)
	}
}

// tryLoadBaseFst attempts to load all base FSTs from disk cache.
// Returns true if all were loaded successfully.
func (p *Processor) tryLoadBaseFst(cacheDir, prefix string) bool {
	basePath := func(name string) string {
		return filepath.Join(cacheDir, prefix+"_base_"+name+".fst")
	}
	type baseEntry struct {
		name string
		dst  **pynini.Fst
	}
	entries := []baseEntry{
		{"vsig", &p.VSIGMA},
		{"char", &p.CHAR},
		{"sigma", &p.SIGMA},
		{"not_quote", &p.NOT_QUOTE},
		{"not_space", &p.NOT_SPACE},
		{"to_lower", &p.TO_LOWER},
		{"to_upper", &p.TO_UPPER},
		{"insert_space", &p.INSERT_SPACE},
		{"delete_space", &p.DELETE_SPACE},
		{"delete_extra_space", &p.DELETE_EXTRA_SPACE},
		{"delete_zero_one_space", &p.DELETE_ZERO_OR_ONE_SPACE},
	}
	for _, e := range entries {
		fst, err := pynini.FstRead(basePath(e.name))
		if err != nil {
			return false
		}
		*e.dst = fst
	}
	p.baseFstLoaded = true
	return true
}

// saveBaseFst saves all base FSTs to disk cache for future use.
func (p *Processor) saveBaseFst(cacheDir, prefix string) {
	basePath := func(name string) string {
		return filepath.Join(cacheDir, prefix+"_base_"+name+".fst")
	}
	save := func(name string, fst *pynini.Fst) {
		if fst != nil {
			if err := pynini.FstWrite(fst, basePath(name)); err != nil {
				// Remove partial/corrupt file so tryLoadBaseFst won't load it
				os.Remove(basePath(name))
			}
		}
	}
	save("vsig", p.VSIGMA)
	save("char", p.CHAR)
	save("sigma", p.SIGMA)
	save("not_quote", p.NOT_QUOTE)
	save("not_space", p.NOT_SPACE)
	save("to_lower", p.TO_LOWER)
	save("to_upper", p.TO_UPPER)
	save("insert_space", p.INSERT_SPACE)
	save("delete_space", p.DELETE_SPACE)
	save("delete_extra_space", p.DELETE_EXTRA_SPACE)
	save("delete_zero_one_space", p.DELETE_ZERO_OR_ONE_SPACE)
}

func (p *Processor) BuildFst(prefix, cacheDir string, overwriteCache bool) {
	p.BuildFstWithCache(prefix, cacheDir, overwriteCache, 0, nil, func() {}, func() {})
}

// loadTagger returns the tagger FST, loading via cache if needed.
// The result is cached in p.taggerRef for fast subsequent access.
func (p *Processor) loadTagger() *pynini.Fst {
	if p.taggerRef != nil {
		return p.taggerRef
	}
	if p.cache != nil && p.cachePrefix != "" {
		fst := p.cache.GetOrBuild(p.cachePrefix+"_tagger", p.taggerBuilder)
		p.taggerRef = fst
		return fst
	}
	return p.Tagger
}

// loadVerbalizer returns the verbalizer FST, loading via cache if needed.
// The result is cached in p.verbalizerRef for fast subsequent access.
func (p *Processor) loadVerbalizer() *pynini.Fst {
	if p.verbalizerRef != nil {
		return p.verbalizerRef
	}
	if p.cache != nil && p.cachePrefix != "" {
		fst := p.cache.GetOrBuild(p.cachePrefix+"_verbalizer", p.verbalizerBuilder)
		p.verbalizerRef = fst
		return fst
	}
	return p.Verbalizer
}

func (p *Processor) Tag(input string) string {
	if len(input) == 0 {
		return ""
	}
	tagger := p.loadTagger()
	if tagger == nil || len(tagger.States) == 0 {
		return ""
	}
	input = pynini.Escape(input)
	return pynini.ComposeInputWithFst(input, nil, tagger)
}

func (p *Processor) Verbalize(input string) string {
	if len(input) == 0 {
		return ""
	}
	verbalizer := p.loadVerbalizer()
	if verbalizer == nil || len(verbalizer.States) == 0 {
		return ""
	}
	output := p.tokenParser.Reorder(input)
	return pynini.ComposeInputWithFst(pynini.Escape(output), nil, verbalizer)
}

func (p *Processor) Normalize(input string) string {
	return p.Verbalize(p.Tag(input))
}

// SetFSTCacheTTL overrides the default 10-minute TTL for the cache manager.
// Only effective if BuildFstWithCache has been called.
func (p *Processor) SetFSTCacheTTL(d time.Duration) {
	if p.cache != nil {
		p.cache.SetTTL(d)
	}
}

// SetCJKVCHAR extends VCHAR with CJK characters from a charset file
// (one character per line). After loading, VSIGMA, CHAR, SIGMA, NOT_QUOTE,
// and NOT_SPACE are rebuilt to include the CJK characters.
// This is needed for Chinese and Japanese text normalization.
func (p *Processor) SetCJKVCHAR(charsetPath string) {
	cjkFst, err := pynini.StringFile(charsetPath)
	if err != nil || cjkFst == nil {
		return
	}
	// Merge CJK chars into VCHAR
	p.VCHAR = pynini.Union(p.VCHAR, cjkFst)
	// Rebuild all derived FSTs
	p.VSIGMA = p.VCHAR.Star()
	CHAR := p.VCHAR.Difference(pynini.Union(pynini.Accep("\\"), pynini.Accep("\"")))
	p.CHAR = pynini.Union(CHAR, pynini.Cross("\\", "\\\\"), pynini.Cross("\"", "\\\""))
	sigmaBase := pynini.Union(CHAR, pynini.Cross("\\\\", "\\"), pynini.Cross("\\\"", "\""))
	p.SIGMA = sigmaBase.Star()
	p.NOT_QUOTE = p.VCHAR.Difference(pynini.Accep("\""))
	p.NOT_SPACE = p.VCHAR.Difference(p.SPACE)
}

// ReleaseBaseFsts releases the large base FSTs that are only needed during
// rule construction (BuildTagger/BuildVerbalizer) but not at runtime
// (Normalize/Tag/Verbalize). After calling this method, the Processor's
// VSIGMA, CHAR, SIGMA, NOT_QUOTE, NOT_SPACE, TO_LOWER, TO_UPPER,
// INSERT_SPACE, DELETE_SPACE, DELETE_EXTRA_SPACE, DELETE_ZERO_OR_ONE_SPACE,
// ALPHA, DIGIT, PUNCT, SPACE, VCHAR, LOWER, UPPER fields are set to nil.
// Name, Ordertype, Tagger, Verbalizer, cache, and tokenParser are preserved.
func (p *Processor) ReleaseBaseFsts() {
	p.VSIGMA = nil
	p.CHAR = nil
	p.SIGMA = nil
	p.NOT_QUOTE = nil
	p.NOT_SPACE = nil
	p.TO_LOWER = nil
	p.TO_UPPER = nil
	p.INSERT_SPACE = nil
	p.DELETE_SPACE = nil
	p.DELETE_EXTRA_SPACE = nil
	p.DELETE_ZERO_OR_ONE_SPACE = nil
	p.ALPHA = nil
	p.DIGIT = nil
	p.PUNCT = nil
	p.SPACE = nil
	p.VCHAR = nil
	p.LOWER = nil
	p.UPPER = nil
}

// Close releases resources held by the Processor, including stopping the
// background cache eviction goroutine. After calling Close, the Processor
// should not be used for Tag/Verbalize operations that require cache access.
// It is safe to call Close multiple times.
func (p *Processor) Close() {
	if p.cache != nil {
		p.cache.StopEviction()
	}
}

// FSTCacheStats returns the number of entries currently in memory cache.
func (p *Processor) FSTCacheStats() (inMemory int, diskSizeMB float64) {
	if p.cache == nil {
		return 0, 0
	}
	return p.cache.Size(), float64(p.cache.DiskSize()) / 1024 / 1024
}