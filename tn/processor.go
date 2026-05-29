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

	// Build configuration
	buildConcurrency int
	buildProgress    BuildProgressFn

	// Lazy base FST initialization
	baseFstLoaded bool
}

func (p *Processor) GetBuildConfig() (concurrency int, progress BuildProgressFn) {
	return p.buildConcurrency, p.buildProgress
}

func NewProcessor(name string, ordertype ...string) *Processor {
	ot := "tn"
	if len(ordertype) > 0 {
		ot = ordertype[0]
	}

	p := &Processor{
		Name:       name,
		Ordertype:  ot,
		ALPHA:      lib.ALPHA,
		DIGIT:      lib.DIGIT,
		PUNCT:      lib.PUNCT,
		SPACE:      pynini.Union(lib.SPACE, pynini.Accep("\u00A0")),
		VCHAR:      lib.VALID_UTF8_CHAR,
		LOWER:      lib.LOWER,
		UPPER:      lib.UPPER,
		MIN_NEG_WEIGHT: -0.0001,
	}
	// Build all base FSTs inline (matching Python Processor.__init__ behavior)
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

	p.baseFstLoaded = true
	return p
}

// buildBaseFst is a no-op now; base FSTs are built in NewProcessor.
func (p *Processor) buildBaseFst() {
	if p.baseFstLoaded {
		return
	}
	p.baseFstLoaded = true
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
			pynini.FstWrite(fst, basePath(name))
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
// When cache is used, p.Tagger is cleared to allow the old FST to be GC'd.
func (p *Processor) loadTagger() *pynini.Fst {
	if p.cache != nil && p.cachePrefix != "" {
		fst := p.cache.GetOrBuild(p.cachePrefix+"_tagger", p.taggerBuilder)
		// Clear p.Tagger so old evicted FSTs can be GC'd.
		// loadTagger is the sole source of truth when cache is active.
		p.Tagger = nil
		return fst
	}
	return p.Tagger
}

// loadVerbalizer returns the verbalizer FST, loading via cache if needed.
func (p *Processor) loadVerbalizer() *pynini.Fst {
	if p.cache != nil && p.cachePrefix != "" {
		fst := p.cache.GetOrBuild(p.cachePrefix+"_verbalizer", p.verbalizerBuilder)
		p.Verbalizer = nil
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
	return pynini.Accep(input).ComposeShortestPath(tagger)
}

func (p *Processor) Verbalize(input string) string {
	if len(input) == 0 {
		return ""
	}
	verbalizer := p.loadVerbalizer()
	if verbalizer == nil || len(verbalizer.States) == 0 {
		return ""
	}
	output := NewTokenParser(p.Ordertype).Reorder(input)
	return pynini.Accep(output).ComposeShortestPath(verbalizer)
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

// FSTCacheStats returns the number of entries currently in memory cache.
func (p *Processor) FSTCacheStats() (inMemory int, diskSizeMB float64) {
	if p.cache == nil {
		return 0, 0
	}
	return p.cache.Size(), float64(p.cache.DiskSize()) / 1024 / 1024
}