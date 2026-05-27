package tn

import (
	"os"
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
	p.INSERT_SPACE = lib.Insert(" ")
	p.DELETE_SPACE = lib.Delete(p.SPACE).Star()
	p.DELETE_EXTRA_SPACE = pynini.Cross(p.SPACE.Plus(), pynini.Accep(" "))
	p.DELETE_ZERO_OR_ONE_SPACE = lib.Delete(p.SPACE).Ques()

	lowercase := "abcdefghijklmnopqrstuvwxyz"
	uppercase := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	var toLowerFsts []*pynini.Fst
	for i := 0; i < len(lowercase); i++ {
		toLowerFsts = append(toLowerFsts, pynini.Cross(string(uppercase[i]), string(lowercase[i])))
	}
	p.TO_LOWER = pynini.Union(toLowerFsts...)
	p.TO_UPPER = p.TO_LOWER.Invert()

	return p
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

// BuildFstWithCache handles cache read/write for tagger and verbalizer using
// the FST cache manager (lazy load + TTL eviction). To skip the cache and
// force a rebuild, set overwriteCache=true or set ttl=0.
func (p *Processor) BuildFstWithCache(prefix, cacheDir string, overwriteCache bool, buildTagger, buildVerbalizer func()) {
	if cacheDir == "" {
		cacheDir = ".cache"
	}
	os.MkdirAll(cacheDir, 0755)

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
		// Clear both memory and disk cache for this prefix
		p.cache.Invalidate(prefix + "_tagger")
		p.cache.Invalidate(prefix + "_verbalizer")
	}

	// Pre-load tagger and verbalizer into memory via cache
	p.Tagger = p.cache.GetOrBuild(prefix+"_tagger", p.taggerBuilder)
	p.Verbalizer = p.cache.GetOrBuild(prefix+"_verbalizer", p.verbalizerBuilder)
}

func (p *Processor) BuildFst(prefix, cacheDir string, overwriteCache bool) {
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