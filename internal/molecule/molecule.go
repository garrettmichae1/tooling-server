// Package molecule turns a small chemistry formula into a ball-and-stick STL.
// The agent sends the formula. The server owns the shape.
package molecule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"edsger.local/figureserver/internal/model"
)

// ErrFormula means the formula is missing or not one of the known models.
var ErrFormula = errors.New("invalid formula")

const (
	mmPerAngstrom = 14
	bondRadius    = 1.4
	sphereFn      = 16
)

// Build reads {"formula":"H2O"} and returns a binary STL.
func Build(ctx context.Context, raw json.RawMessage, c model.Compiler) ([]byte, error) {
	formula, err := parseRequest(raw)
	if err != nil {
		return nil, ErrFormula
	}
	mol, ok := lookup(formula)
	if !ok {
		return nil, ErrFormula
	}
	return c.Render(ctx, scad(mol))
}

func parseRequest(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", ErrFormula
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var in struct {
		Formula string `json:"formula"`
	}
	if err := dec.Decode(&in); err != nil {
		return "", ErrFormula
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return "", ErrFormula
	}
	formula := strings.TrimSpace(in.Formula)
	if formula == "" || len(formula) > 40 || !utf8.ValidString(formula) || strings.ContainsRune(formula, 0) {
		return "", ErrFormula
	}
	if strings.ContainsAny(formula, "#`\\") || strings.Contains(formula, "..") {
		return "", ErrFormula
	}
	return formula, nil
}

type atom struct {
	el string
	x  float64
	y  float64
	z  float64
}

type mol struct {
	atoms []atom
	bonds [][2]int
}

func lookup(formula string) (mol, bool) {
	key, err := canonical(formula)
	if err != nil {
		return mol{}, false
	}
	m, ok := catalog[key]
	return m, ok
}

func canonical(formula string) (string, error) {
	counts, err := parseFormula(formula)
	if err != nil || len(counts) == 0 {
		return "", ErrFormula
	}
	return countKey(counts), nil
}

func parseFormula(s string) (map[string]int, error) {
	counts, rest, err := parseGroup(s)
	if err != nil || rest != "" {
		return nil, ErrFormula
	}
	return counts, nil
}

func parseGroup(s string) (map[string]int, string, error) {
	counts := map[string]int{}
	for len(s) > 0 {
		if s[0] == ')' {
			return counts, s, nil
		}
		if s[0] == '(' {
			inner, rest, err := parseGroup(s[1:])
			if err != nil || !strings.HasPrefix(rest, ")") {
				return nil, "", ErrFormula
			}
			rest = rest[1:]
			n, rest, err := parseCount(rest)
			if err != nil {
				return nil, "", err
			}
			for el, c := range inner {
				counts[el] += c * n
			}
			s = rest
			continue
		}
		el, rest, err := parseElement(s)
		if err != nil {
			return nil, "", err
		}
		n, rest, err := parseCount(rest)
		if err != nil {
			return nil, "", err
		}
		counts[el] += n
		s = rest
	}
	return counts, "", nil
}

var twoLetter = map[string]bool{"Br": true, "Cl": true, "Na": true}

func parseElement(s string) (string, string, error) {
	r, size := utf8.DecodeRuneInString(s)
	if !unicode.IsLetter(r) {
		return "", "", ErrFormula
	}
	rest := s[size:]
	el := string(unicode.ToUpper(r))
	if rest != "" {
		r2, size2 := utf8.DecodeRuneInString(rest)
		if unicode.IsLetter(r2) && unicode.IsLower(r2) {
			pair := el + string(r2)
			if twoLetter[pair] {
				return pair, rest[size2:], nil
			}
		}
	}
	return el, rest, nil
}

func parseCount(s string) (int, string, error) {
	if s == "" || s[0] < '0' || s[0] > '9' {
		return 1, s, nil
	}
	n := 0
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int(s[i]-'0')
		i++
		if n > 30 {
			return 0, "", ErrFormula
		}
	}
	if n == 0 {
		return 0, "", ErrFormula
	}
	return n, s[i:], nil
}

func countKey(counts map[string]int) string {
	order := make([]string, 0, len(counts))
	if counts["C"] > 0 {
		order = append(order, "C")
	}
	if counts["H"] > 0 {
		order = append(order, "H")
	}
	rest := make([]string, 0, len(counts))
	for el := range counts {
		if el != "C" && el != "H" {
			rest = append(rest, el)
		}
	}
	sortStrings(rest)
	order = append(order, rest...)
	var b strings.Builder
	for _, el := range order {
		fmt.Fprintf(&b, "%s%d", el, counts[el])
	}
	return b.String()
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

var ballRadius = map[string]float64{
	"H": 4, "C": 6.5, "N": 6.2, "O": 6, "F": 5.5,
	"Na": 8, "P": 7, "S": 7.2, "Cl": 7.5, "Br": 8,
}

func scad(m mol) string {
	var b strings.Builder
	fmt.Fprintf(&b, "$fn = %d;\n", sphereFn)
	for _, bond := range m.bonds {
		a := m.atoms[bond[0]]
		c := m.atoms[bond[1]]
		fmt.Fprintf(&b, "hull() {\n  translate([%.3f, %.3f, %.3f]) sphere(%.3f);\n  translate([%.3f, %.3f, %.3f]) sphere(%.3f);\n}\n",
			a.x*mmPerAngstrom, a.y*mmPerAngstrom, a.z*mmPerAngstrom, bondRadius,
			c.x*mmPerAngstrom, c.y*mmPerAngstrom, c.z*mmPerAngstrom, bondRadius)
	}
	for _, a := range m.atoms {
		r := ballRadius[a.el]
		fmt.Fprintf(&b, "translate([%.3f, %.3f, %.3f]) sphere(%.3f);\n",
			a.x*mmPerAngstrom, a.y*mmPerAngstrom, a.z*mmPerAngstrom, r)
	}
	return b.String()
}

func at(el string, x, y, z float64) atom {
	return atom{el: el, x: x, y: y, z: z}
}

func stick(pairs ...[2]int) [][2]int { return pairs }

// catalog keys are Hill-order counts. Coordinates are angstroms.
var catalog = map[string]mol{
	"H2":    {atoms: []atom{at("H", 0, 0, 0), at("H", 0.74, 0, 0)}, bonds: stick([2]int{0, 1})},
	"N2":    {atoms: []atom{at("N", 0, 0, 0), at("N", 1.10, 0, 0)}, bonds: stick([2]int{0, 1})},
	"O2":    {atoms: []atom{at("O", 0, 0, 0), at("O", 1.21, 0, 0)}, bonds: stick([2]int{0, 1})},
	"H1F1":  {atoms: []atom{at("F", 0, 0, 0), at("H", 0.92, 0, 0)}, bonds: stick([2]int{0, 1})},
	"H1Cl1": {atoms: []atom{at("Cl", 0, 0, 0), at("H", 1.27, 0, 0)}, bonds: stick([2]int{0, 1})},
	"H2O1": {
		atoms: []atom{at("O", 0, 0, 0), at("H", 0.96, 0, 0), at("H", -0.24, 0.93, 0)},
		bonds: stick([2]int{0, 1}, [2]int{0, 2}),
	},
	"C1O2": {
		atoms: []atom{at("C", 0, 0, 0), at("O", -1.16, 0, 0), at("O", 1.16, 0, 0)},
		bonds: stick([2]int{0, 1}, [2]int{0, 2}),
	},
	"H3N1": {
		atoms: []atom{
			at("N", 0, 0, 0),
			at("H", 0.94, 0, -0.38),
			at("H", -0.47, 0.81, -0.38),
			at("H", -0.47, -0.81, -0.38),
		},
		bonds: stick([2]int{0, 1}, [2]int{0, 2}, [2]int{0, 3}),
	},
	"C1H4": {
		atoms: []atom{
			at("C", 0, 0, 0),
			at("H", 0.63, 0.63, 0.63),
			at("H", 0.63, -0.63, -0.63),
			at("H", -0.63, 0.63, -0.63),
			at("H", -0.63, -0.63, 0.63),
		},
		bonds: stick([2]int{0, 1}, [2]int{0, 2}, [2]int{0, 3}, [2]int{0, 4}),
	},
	"H2O2": {
		atoms: []atom{
			at("O", 0, 0, 0),
			at("O", 1.47, 0, 0),
			at("H", -0.20, 0.93, 0),
			at("H", 1.67, -0.35, 0.86),
		},
		bonds: stick([2]int{0, 1}, [2]int{0, 2}, [2]int{1, 3}),
	},
	"O2S1": {
		atoms: []atom{at("S", 0, 0, 0), at("O", 1.43, 0, 0), at("O", -0.69, 1.25, 0)},
		bonds: stick([2]int{0, 1}, [2]int{0, 2}),
	},
	"C1H4O1": {
		atoms: []atom{
			at("C", 0, 0, 0),
			at("O", 1.43, 0, 0),
			at("H", -0.36, 1.02, 0),
			at("H", -0.36, -0.51, 0.89),
			at("H", -0.36, -0.51, -0.89),
			at("H", 1.80, 0.82, 0),
		},
		bonds: stick([2]int{0, 1}, [2]int{0, 2}, [2]int{0, 3}, [2]int{0, 4}, [2]int{1, 5}),
	},
	"C2H6": {
		atoms: []atom{
			at("C", 0, 0, 0),
			at("C", 1.54, 0, 0),
			at("H", -0.36, 1.02, 0),
			at("H", -0.36, -0.51, 0.89),
			at("H", -0.36, -0.51, -0.89),
			at("H", 1.90, 0.51, 0.89),
			at("H", 1.90, 0.51, -0.89),
			at("H", 1.90, -1.02, 0),
		},
		bonds: stick([2]int{0, 1}, [2]int{0, 2}, [2]int{0, 3}, [2]int{0, 4}, [2]int{1, 5}, [2]int{1, 6}, [2]int{1, 7}),
	},
	"C2H4": {
		atoms: []atom{
			at("C", 0, 0, 0),
			at("C", 1.34, 0, 0),
			at("H", -0.58, 0.93, 0),
			at("H", -0.58, -0.93, 0),
			at("H", 1.92, 0.93, 0),
			at("H", 1.92, -0.93, 0),
		},
		bonds: stick([2]int{0, 1}, [2]int{0, 2}, [2]int{0, 3}, [2]int{1, 4}, [2]int{1, 5}),
	},
	"C2H2": {
		atoms: []atom{
			at("C", 0, 0, 0),
			at("C", 1.20, 0, 0),
			at("H", -1.06, 0, 0),
			at("H", 2.26, 0, 0),
		},
		bonds: stick([2]int{0, 1}, [2]int{0, 2}, [2]int{1, 3}),
	},
	"C6H6":   benzene(),
	"H2O4S1": sulfuric(),
}

func benzene() mol {
	var atoms []atom
	for i := 0; i < 6; i++ {
		deg := float64(i) * 60
		rad := deg * 0.0174533
		atoms = append(atoms, at("C", 1.40*math.Cos(rad), 1.40*math.Sin(rad), 0))
	}
	for i := 0; i < 6; i++ {
		deg := float64(i) * 60
		rad := deg * 0.0174533
		atoms = append(atoms, at("H", 2.48*math.Cos(rad), 2.48*math.Sin(rad), 0))
	}
	bonds := make([][2]int, 0, 12)
	for i := 0; i < 6; i++ {
		bonds = append(bonds, [2]int{i, (i + 1) % 6})
		bonds = append(bonds, [2]int{i, i + 6})
	}
	return mol{atoms: atoms, bonds: bonds}
}

func sulfuric() mol {
	// Sulfur, two double-bonded oxygens, two OH groups, roughly tetrahedral.
	return mol{
		atoms: []atom{
			at("S", 0, 0, 0),
			at("O", 0.82, 0.82, 0.82),
			at("O", 0.82, -0.82, -0.82),
			at("O", -0.90, 0.90, -0.90),
			at("O", -0.90, -0.90, 0.90),
			at("H", -1.55, 1.20, -1.40),
			at("H", -1.55, -1.20, 1.40),
		},
		bonds: stick([2]int{0, 1}, [2]int{0, 2}, [2]int{0, 3}, [2]int{0, 4}, [2]int{3, 5}, [2]int{4, 6}),
	}
}
