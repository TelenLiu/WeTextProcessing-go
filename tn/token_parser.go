package tn

import (
	"strings"
)

const EOS = "<EOS>"

var TN_ORDERS = map[string][]string{
	"date":     {"year", "month", "day"},
	"fraction": {"denominator", "numerator"},
	"measure":  {"denominator", "numerator", "value"},
	"money":    {"value", "currency"},
	"time":     {"noon", "hour", "minute", "second"},
}

var EN_TN_ORDERS = map[string][]string{
	"date":  {"preserve_order", "text", "day", "month", "year"},
	"money": {"integer_part", "fractional_part", "quantity", "currency_maj"},
}

var ITN_ORDERS = map[string][]string{
	"date":     {"year", "month", "day"},
	"fraction": {"sign", "numerator", "denominator"},
	"measure":  {"numerator", "denominator", "value"},
	"money":    {"currency", "value", "decimal"},
	"time":     {"hour", "minute", "second", "noon"},
}

type Token struct {
	Name    string
	Order   []string
	Members map[string]string
}

func NewToken(name string) *Token {
	return &Token{
		Name:    name,
		Order:   []string{},
		Members: make(map[string]string),
	}
}

func (t *Token) Append(key, value string) {
	t.Order = append(t.Order, key)
	t.Members[key] = value
}

func (t *Token) String(orders map[string][]string) string {
	output := t.Name + " {"
	if orderKeys, exists := orders[t.Name]; exists {
		if preserveOrder, hasPreserve := t.Members["preserve_order"]; !hasPreserve || preserveOrder != "true" {
			t.Order = orderKeys
		}
	}

	for _, key := range t.Order {
		if value, exists := t.Members[key]; exists {
			output += " " + key + ": \"" + value + "\""
		}
	}
	return output + " }"
}

type TokenParser struct {
	orders map[string][]string
	index  int
	runes  []rune
	char   string
	tokens []*Token
}

func NewTokenParser(ordertype string) *TokenParser {
	var orders map[string][]string
	switch ordertype {
	case "tn":
		orders = TN_ORDERS
	case "itn":
		orders = ITN_ORDERS
	case "en_tn":
		orders = EN_TN_ORDERS
	default:
		orders = TN_ORDERS
	}
	return &TokenParser{
		orders: orders,
	}
}

func (tp *TokenParser) load(input string) {
	tp.index = 0
	tp.runes = []rune(input)
	if len(tp.runes) > 0 {
		tp.char = string(tp.runes[0])
	} else {
		tp.char = EOS
	}
	tp.tokens = []*Token{}
}

func (tp *TokenParser) read() bool {
	if tp.index < len(tp.runes)-1 {
		tp.index++
		tp.char = string(tp.runes[tp.index])
		return true
	}
	tp.char = EOS
	return false
}

func (tp *TokenParser) parseWs() bool {
	notEos := tp.char != EOS
	for notEos && tp.char == " " {
		notEos = tp.read()
	}
	return notEos
}

func (tp *TokenParser) parseChar(exp string) bool {
	if tp.char == exp {
		tp.read()
		return true
	}
	return false
}

func (tp *TokenParser) parseChars(exp string) bool {
	ok := false
	for _, x := range exp {
		// Use | instead of || to avoid short-circuiting - we need to advance
		// the parser for each character, matching Python's |= behavior
		if tp.parseChar(string(x)) {
			ok = true
		}
	}
	return ok
}

func (tp *TokenParser) parseKey() string {
	key := ""
	for tp.char != EOS {
		ch := tp.char
		if (ch >= "a" && ch <= "z") || (ch >= "A" && ch <= "Z") || ch == "_" {
			key += ch
			tp.read()
		} else {
			break
		}
	}
	return key
}

func (tp *TokenParser) parseValue() string {
	escape := false
	value := ""
	for tp.char != "\"" && tp.char != EOS {
		value += tp.char
		escape = tp.char == "\\"
		tp.read()
		if escape && tp.char != EOS {
			escape = false
			value += tp.char
			tp.read()
		}
	}
	return value
}

func (tp *TokenParser) parse(input string) {
	tp.load(input)
	for tp.parseWs() {
		name := tp.parseKey()
		tp.parseChars(" { ")

		token := NewToken(name)
		for tp.parseWs() {
			if tp.char == "}" {
				tp.parseChar("}")
				break
			}
			key := tp.parseKey()
			tp.parseChars(": \"")
			value := tp.parseValue()
			tp.parseChar("\"")
			token.Append(key, value)
		}
		tp.tokens = append(tp.tokens, token)
	}
}

func (tp *TokenParser) Reorder(input string) string {
	tp.parse(input)
	output := ""
	for _, token := range tp.tokens {
		output += token.String(tp.orders) + " "
	}
	return strings.TrimSpace(output)
}
