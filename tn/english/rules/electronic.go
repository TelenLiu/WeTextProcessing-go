package rules

import (
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini/lib"
	"github.com/TelenLiu/WeTextProcessing-go/tn"
)

type Electronic struct {
	*tn.Processor
	deterministic bool
}

func NewElectronic(args ...bool) *Electronic {
	deterministic := false
	if len(args) > 0 {
		deterministic = args[0]
	}
	e := &Electronic{
		Processor:   tn.NewProcessor("electronic", "en_tn"),
		deterministic: deterministic,
	}
	e.BuildTagger()
	e.BuildVerbalizer()
	return e
}

func (e *Electronic) BuildTagger() {
	cardinal := getSharedCardinal(e.deterministic)
	var numbers *pynini.Fst
	if e.deterministic {
		numbers = e.DIGIT
	} else {
		numbers = lib.Insert(" ").Concat(cardinal.LongNumbers).Concat(lib.Insert(" "))
	}

	acceptedSymbols, _ := pynini.StringFile(tn.EnglishDataPath("data/electronic/symbol.tsv"))
	acceptedSymbolsInput := pynini.Project(acceptedSymbols, "input")
	acceptedDomains, _ := pynini.StringFile(tn.EnglishDataPath("data/electronic/domain.tsv"))
	acceptedDomainsInput := pynini.Project(acceptedDomains, "input")

	dictWords := lib.AddWeight(pynini.StringFileMust(tn.EnglishDataPath("data/electronic/words.tsv")), -0.0001)
	dictWordsWithoutDelimiter := dictWords.Concat(
		lib.AddWeight(lib.Insert(" ").Concat(dictWords), -0.0001).Plus())
	dictWordsGraph := dictWordsWithoutDelimiter.Union(dictWords)

	allAcceptedSymbolsStart := pynini.Union(dictWordsGraph, e.ALPHA.Star(), acceptedSymbolsInput).Optimize()
	allAcceptedSymbolsEnd := pynini.Union(dictWordsGraph, numbers, e.ALPHA.Star(), acceptedSymbolsInput).Optimize()

	graphSymbols, _ := pynini.StringFile(tn.EnglishDataPath("data/electronic/symbol.tsv"))
	graphSymbols = graphSymbols.Optimize()

	username := e.ALPHA.Union(dictWordsGraph).Concat(
		e.ALPHA.Union(numbers).Union(acceptedSymbolsInput).Union(dictWordsGraph).Star())
	username = lib.Insert("username: \"").Concat(username).Concat(lib.Insert("\"")).Concat(pynini.Cross("@", " "))

	domainGraph := allAcceptedSymbolsStart.Concat(
		allAcceptedSymbolsEnd.Union(
			lib.AddWeight(acceptedDomainsInput, -0.0001)).Star())

	protocolSymbols := graphSymbols.Union(pynini.Cross(":", "colon")).Concat(lib.Insert(" ")).Star()
	protocolStart := pynini.Union(
		pynini.Cross("https", "HTTPS "),
		pynini.Cross("http", "HTTP "),
	).Concat(pynini.Accep("://").Compose(protocolSymbols))
	protocolFileStart := pynini.Accep("file").Concat(e.INSERT_SPACE).Concat(
		pynini.Accep(":///").Compose(protocolSymbols))
	protocolEnd := lib.AddWeight(pynini.Cross("www", "WWW ").Concat(pynini.Accep(".").Compose(protocolSymbols)), -1000)
	protocol := pynini.Union(
		protocolFileStart, protocolStart, protocolEnd,
		protocolStart.Concat(protocolEnd),
	)

	// domainGraphWithClass: instead of using Compose with NOT_SPACE.Star() which
	// causes state explosion, we directly build the domain graph with ALPHA prefix.
	// Python: compose(ALPHA + NOT_SPACE.star + (ALPHA|DIGIT|accep("/")), domain_graph)
	// Go: We use ALPHA.Plus() as a simpler pattern that still matches domain names
	// starting with a letter, then Compose with domain_graph which is much smaller
	// than NOT_SPACE.Star().
	domainPrefix := e.ALPHA.Concat(e.ALPHA.Union(e.DIGIT).Union(pynini.Accep("/")).Union(pynini.Accep(".")).Union(pynini.Accep("-")).Star())
	domainGraphWithClass := lib.Insert("domain: \"").Concat(
		domainPrefix.Compose(domainGraph).Optimize()).Concat(lib.Insert("\""))

	protocol = lib.Insert("protocol: \"").Concat(lib.AddWeight(protocol, -0.0001)).Concat(lib.Insert("\""))

	// email: Instead of VCHAR.Star() Compose which causes state explosion,
	// we directly use username + domainGraphWithClass since the Compose with
	// VCHAR.Star() is just a pattern filter that's already handled by the
	// username and domain graphs themselves.
	// Python: compose(VCHAR.star + accep("@") + VCHAR.star + accep(".") + VCHAR.star, username + domain)
	// Go: Directly build the email pattern without VCHAR.Star() wrapper.
	graph := username.Concat(domainGraphWithClass)

	// domain only: Python requires the input to contain accepted_common_domains
	// (e.g., ".com", ".edu") to avoid matching arbitrary strings like "Dr."
	// Python: compose(ALPHA + NOT_SPACE.star + accepted_common_domains + NOT_SPACE.star, domain_graph)
	// We use acceptedDomainsInput (projected input labels) as the required suffix.
	domainOnlyPrefix := e.ALPHA.Concat(e.ALPHA.Union(e.DIGIT).Union(pynini.Accep("/")).Union(pynini.Accep(".")).Union(pynini.Accep("-")).Star()).Concat(acceptedDomainsInput)
	graph = graph.Union(
		lib.Insert("domain: \"").Concat(
			domainOnlyPrefix.Compose(domainGraph).Optimize()).Concat(lib.Insert("\"")),
	)

	// with protocol
	graph = graph.Union(
		protocol.Concat(lib.Insert(" ")).Concat(domainGraphWithClass),
	)

	// RmEpsilon+Connect: eliminate epsilon arcs from Union and Insert wrappers
	// before AddTokens. This reduces epsilon closure BFS cost at runtime.
	graph = graph.RmEpsilon().Connect()
	finalGraph := e.AddTokens(graph)
	e.Tagger = finalGraph.Optimize()
}

func (e *Electronic) BuildVerbalizer() {
	graphDigitNoZero, _ := pynini.StringFile(tn.EnglishDataPath("data/number/digit.tsv"))
	graphDigitNoZero = graphDigitNoZero.Invert().Optimize()
	graphZero := pynini.Cross("0", "zero")

	if !e.deterministic {
		graphZero = graphZero.Union(pynini.Cross("0", "o")).Union(pynini.Cross("0", "oh"))
	}

	graphDigit := graphDigitNoZero.Union(graphZero)
	graphSymbols, _ := pynini.StringFile(tn.EnglishDataPath("data/electronic/symbol.tsv"))
	graphSymbols = graphSymbols.Optimize()

	nemoNotBracket := e.VCHAR.Difference(pynini.Union(pynini.Accep("{"), pynini.Accep("}"))).Optimize()

	dictWords, _ := pynini.StringFile(tn.EnglishDataPath("data/electronic/words.tsv"))
	dictWordsOutput := pynini.Project(dictWords, "output")

	defaultCharsSymbols := e.BuildRule(
		lib.Insert(" ").Concat(graphSymbols.Union(graphDigit)).Concat(lib.Insert(" ")),
		"", "")
	defaultCharsSymbols = defaultCharsSymbols.Compose(nemoNotBracket.Star()).Optimize()

	spaceSeparatedDictWords := lib.AddWeight(
		e.ALPHA.Concat(e.ALPHA.Union(pynini.Accep(" ")).Star()).Concat(
			pynini.Accep(" ")).Concat(e.ALPHA.Union(pynini.Accep(" ")).Star()),
		-0.0001)

	userName := lib.DeleteString("username:").Concat(e.DELETE_SPACE).Concat(
		lib.DeleteString("\"")).Concat(
		defaultCharsSymbols.Union(spaceSeparatedDictWords).Optimize()).Concat(lib.DeleteString("\""))

	domainCommon, _ := pynini.StringFile(tn.EnglishDataPath("data/electronic/domain.tsv"))

	domainAll := defaultCharsSymbols.Compose(
		e.ALPHA.Union(pynini.Accep(" ")).Union(lib.AddWeight(dictWordsOutput, -0.0001)).Star())

	domain := domainAll.Concat(e.INSERT_SPACE).Concat(
		domainCommon.Union(pynini.Cross(".", "dot"))).Concat(
		e.INSERT_SPACE.Concat(defaultCharsSymbols).Ques())
	domain = lib.DeleteString("domain:").Concat(e.DELETE_SPACE).Concat(lib.DeleteString("\"")).Concat(
		domain.Union(lib.AddWeight(domainAll, 100))).Optimize().Concat(e.DELETE_SPACE).Concat(lib.DeleteString("\""))

	protocol := lib.DeleteString("protocol: \"").Concat(e.NOT_QUOTE.Plus()).Concat(lib.DeleteString("\""))

	graph := protocol.Concat(e.DELETE_SPACE).Ques().Concat(
		userName.Concat(e.DELETE_SPACE).Concat(lib.Insert(" at ")).Concat(e.DELETE_SPACE)).Ques().Concat(
		domain).Concat(e.DELETE_SPACE)
	graph = graph.Compose(
		e.BuildRule(e.DELETE_EXTRA_SPACE, "", ""),
	)

	deleteTokens := e.DeleteTokens(graph)
	e.Verbalizer = deleteTokens.Optimize()
}
