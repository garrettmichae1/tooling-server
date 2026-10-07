package tools

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// This parser accepts a small arithmetic language, never Go/Python/JS source.
type expressionParser struct {
	text        string
	pos, tokens int
}
type lexeme struct {
	kind byte
	text string
}

func calculate(raw json.RawMessage) (any, error) {
	var in struct {
		Expression string `json:"expression"`
	}
	decode(raw, &in)
	p := expressionParser{text: in.Expression}
	value, err := p.expression(0, 0)
	if err != nil {
		return nil, err
	}
	next, err := p.next()
	if err != nil {
		return nil, err
	}
	if next.kind != 0 {
		return nil, invalid("arguments.expression", "unexpected token after expression")
	}
	return map[string]any{"value": value, "precision": "float64", "angle_unit": "radians"}, nil
}

func (p *expressionParser) next() (lexeme, error) {
	for p.pos < len(p.text) && unicode.IsSpace(rune(p.text[p.pos])) {
		p.pos++
	}
	if p.pos == len(p.text) {
		return lexeme{}, nil
	}
	p.tokens++
	if p.tokens > 256 {
		return lexeme{}, invalid("arguments.expression", "too many expression tokens")
	}
	start := p.pos
	c := p.text[p.pos]
	p.pos++
	if strings.ContainsRune("+-*/%^(),", rune(c)) {
		if c == '*' && p.pos < len(p.text) && p.text[p.pos] == '*' {
			p.pos++
			c = '^'
		}
		return lexeme{c, string(c)}, nil
	}
	if c >= '0' && c <= '9' || c == '.' {
		for p.pos < len(p.text) && (p.text[p.pos] >= '0' && p.text[p.pos] <= '9' || p.text[p.pos] == '.') {
			p.pos++
		}
		if p.pos < len(p.text) && (p.text[p.pos] == 'e' || p.text[p.pos] == 'E') {
			p.pos++
			if p.pos < len(p.text) && (p.text[p.pos] == '+' || p.text[p.pos] == '-') {
				p.pos++
			}
			for p.pos < len(p.text) && p.text[p.pos] >= '0' && p.text[p.pos] <= '9' {
				p.pos++
			}
		}
		return lexeme{'n', p.text[start:p.pos]}, nil
	}
	if c >= 'a' && c <= 'z' {
		for p.pos < len(p.text) && (p.text[p.pos] >= 'a' && p.text[p.pos] <= 'z' || p.text[p.pos] >= '0' && p.text[p.pos] <= '9') {
			p.pos++
		}
		return lexeme{'i', p.text[start:p.pos]}, nil
	}
	return lexeme{}, invalid("arguments.expression", "unsupported character; use arithmetic and named functions only")
}

func (p *expressionParser) peek() (lexeme, error) {
	pos, tokens := p.pos, p.tokens
	t, err := p.next()
	p.pos, p.tokens = pos, tokens
	return t, err
}

func (p *expressionParser) expression(minPrec, depth int) (float64, error) {
	if depth > 32 {
		return 0, invalid("arguments.expression", "expression nesting exceeds 32 levels")
	}
	t, err := p.next()
	if err != nil {
		return 0, err
	}
	var left float64
	switch t.kind {
	case 'n':
		left, err = strconv.ParseFloat(t.text, 64)
		if err != nil {
			return 0, invalid("arguments.expression", "invalid or out-of-range number")
		}
	case '+', '-':
		left, err = p.expression(3, depth+1)
		if t.kind == '-' {
			left = -left
		}
	case '(':
		left, err = p.expression(0, depth+1)
		if err == nil {
			err = p.expect(')')
		}
	case 'i':
		switch t.text {
		case "pi":
			left = math.Pi
		case "e":
			left = math.E
		default:
			left, err = p.function(t.text, depth+1)
		}
	default:
		return 0, invalid("arguments.expression", "expected a number, constant, function, or parenthesized expression")
	}
	if err != nil {
		return 0, err
	}
	if !finite(left) {
		return 0, invalid("arguments.expression", "result is outside the finite real-number domain")
	}
	for {
		t, err = p.peek()
		if err != nil {
			return 0, err
		}
		prec := precedence(t.kind)
		if prec < minPrec || prec == 0 {
			break
		}
		_, _ = p.next()
		nextPrec := prec + 1
		if t.kind == '^' {
			nextPrec = prec
		}
		right, err := p.expression(nextPrec, depth+1)
		if err != nil {
			return 0, err
		}
		switch t.kind {
		case '+':
			left += right
		case '-':
			left -= right
		case '*':
			left *= right
		case '/', '%':
			if right == 0 {
				return 0, invalid("arguments.expression", "division or remainder by zero")
			}
			if t.kind == '/' {
				left /= right
			} else {
				left = math.Mod(left, right)
			}
		case '^':
			left = math.Pow(left, right)
		}
		if !finite(left) {
			return 0, invalid("arguments.expression", "result is outside the finite real-number domain")
		}
	}
	return left, nil
}

func precedence(kind byte) int {
	switch kind {
	case '+', '-':
		return 1
	case '*', '/', '%':
		return 2
	case '^':
		return 4
	}
	return 0
}
func (p *expressionParser) expect(kind byte) error {
	t, err := p.next()
	if err != nil {
		return err
	}
	if t.kind != kind {
		return invalid("arguments.expression", "missing parenthesis or separator")
	}
	return nil
}

func (p *expressionParser) function(name string, depth int) (float64, error) {
	one := map[string]func(float64) float64{
		"sqrt": math.Sqrt, "abs": math.Abs, "ln": math.Log, "log10": math.Log10,
		"sin": math.Sin, "cos": math.Cos, "tan": math.Tan, "exp": math.Exp,
		"floor": math.Floor, "ceil": math.Ceil, "round": math.Round,
	}
	f, unary := one[name]
	binary := name == "pow" || name == "min" || name == "max"
	if !unary && !binary {
		return 0, invalid("arguments.expression", "unknown function; use functions listed in the tool description")
	}
	if err := p.expect('('); err != nil {
		return 0, err
	}
	a, err := p.expression(0, depth)
	if err != nil {
		return 0, err
	}
	if unary {
		if err := p.expect(')'); err != nil {
			return 0, err
		}
		return f(a), nil
	}
	if err := p.expect(','); err != nil {
		return 0, err
	}
	b, err := p.expression(0, depth)
	if err != nil {
		return 0, err
	}
	if err := p.expect(')'); err != nil {
		return 0, err
	}
	switch name {
	case "pow":
		return math.Pow(a, b), nil
	case "min":
		return math.Min(a, b), nil
	default:
		return math.Max(a, b), nil
	}
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
