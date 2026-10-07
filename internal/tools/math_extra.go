package tools

import (
	"encoding/json"
	"math"
	"math/big"
	"strconv"
	"strings"
)

func solveQuadratic(raw json.RawMessage) (any, error) {
	var in struct{ A, B, C float64 }
	decode(raw, &in)
	if in.A == 0 {
		if in.B == 0 {
			status := "none"
			if in.C == 0 {
				status = "all_real"
			}
			return map[string]any{"kind": "constant", "status": status, "roots": []float64{}}, nil
		}
		root := -in.C / in.B
		if !finite(root) {
			return nil, invalid("arguments", "linear root exceeds finite numeric limits")
		}
		return map[string]any{"kind": "linear", "status": "one_real", "roots": []float64{root}}, nil
	}
	// Exact arithmetic on float64 representations prevents cancellation from
	// giving the wrong discriminant sign near a repeated root.
	a, b, c := new(big.Rat).SetFloat64(in.A), new(big.Rat).SetFloat64(in.B), new(big.Rat).SetFloat64(in.C)
	d := new(big.Rat).Sub(new(big.Rat).Mul(b, b), new(big.Rat).Mul(big.NewRat(4, 1), new(big.Rat).Mul(a, c)))
	disc, _ := d.Float64()
	// Keep the square root in a wider exponent range: squaring tiny but valid
	// coefficients can underflow float64 even when both roots are ordinary.
	wide := func() *big.Float { return new(big.Float).SetPrec(256) }
	absolute := new(big.Rat).Abs(d)
	wideDisc := wide().SetRat(absolute)
	rootDisc := wide().Sqrt(wideDisc)
	if d.Sign() < 0 {
		real := -in.B / (2 * in.A)
		imag, _ := wide().Quo(rootDisc, wide().SetFloat64(2*math.Abs(in.A))).Float64()
		if !finite(real) || !finite(imag) {
			return nil, invalid("arguments", "roots exceed finite numeric limits")
		}
		return map[string]any{"kind": "quadratic", "status": "two_complex", "discriminant": disc, "discriminant_sign": d.Sign(), "roots": []map[string]float64{{"real": real, "imaginary": imag}, {"real": real, "imaginary": -imag}}}, nil
	}
	if d.Sign() == 0 {
		root := -in.B / (2 * in.A)
		if !finite(root) {
			return nil, invalid("arguments", "root exceeds finite numeric limits")
		}
		return map[string]any{"kind": "quadratic", "status": "one_real", "discriminant": disc, "discriminant_sign": 0, "roots": []float64{root}}, nil
	}
	if math.Signbit(in.B) {
		rootDisc.Neg(rootDisc)
	}
	q := wide().Add(wide().SetFloat64(in.B), rootDisc)
	q.Mul(q, wide().SetFloat64(-.5))
	x1, _ := wide().Quo(q, wide().SetFloat64(in.A)).Float64()
	x2, _ := wide().Quo(wide().SetFloat64(in.C), q).Float64()
	if !finite(x1) || !finite(x2) {
		return nil, invalid("arguments", "roots exceed finite numeric limits")
	}
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	return map[string]any{"kind": "quadratic", "status": "two_real", "discriminant": disc, "discriminant_sign": d.Sign(), "roots": []float64{x1, x2}}, nil
}

func linearRegression(raw json.RawMessage) (any, error) {
	var in struct {
		Points [][]float64 `json:"points"`
	}
	decode(raw, &in)
	xScale, yScale := 0.0, 0.0
	for _, p := range in.Points {
		xScale = math.Max(xScale, math.Abs(p[0]))
		yScale = math.Max(yScale, math.Abs(p[1]))
	}
	if xScale == 0 {
		return nil, invalid("arguments.points", "x values must not all be equal")
	}
	if yScale == 0 {
		yScale = 1
	}
	var mx, my, sxx, syy, sxy float64
	for i, p := range in.Points {
		x, y := p[0]/xScale, p[1]/yScale
		dx, dy := x-mx, y-my
		n := float64(i + 1)
		mx += dx / n
		my += dy / n
		sxx += dx * (x - mx)
		syy += dy * (y - my)
		sxy += dx * (y - my)
	}
	if sxx <= 0 {
		return nil, invalid("arguments.points", "x values must not all be equal")
	}
	scaledSlope := sxy / sxx
	wide := func() *big.Float { return new(big.Float).SetPrec(128) }
	slope, _ := wide().Quo(wide().Mul(wide().SetFloat64(scaledSlope), wide().SetFloat64(yScale)), wide().SetFloat64(xScale)).Float64()
	intercept := (my - scaledSlope*mx) * yScale
	var squared float64
	for _, p := range in.Points {
		residual := p[1]/yScale - (my + scaledSlope*(p[0]/xScale-mx))
		squared += residual * residual
	}
	var corr, r2 *float64
	if syy > 0 {
		r := sxy / math.Sqrt(sxx) / math.Sqrt(syy)
		r = math.Max(-1, math.Min(1, r))
		square := r * r
		corr = &r
		r2 = &square
	}
	rmse := math.Sqrt(squared/float64(len(in.Points))) * yScale
	if !finite(slope) || !finite(intercept) || !finite(rmse) {
		return nil, invalid("arguments.points", "numeric range is too extreme for a finite fit")
	}
	return map[string]any{"count": len(in.Points), "slope": slope, "intercept": intercept, "correlation": corr, "r_squared": r2, "rmse": rmse}, nil
}

func solveLinearSystem(raw json.RawMessage) (any, error) {
	// Float64 is exact for the bounded integer inputs, including JSON 1.0.
	var in struct {
		Matrix    [][]float64 `json:"matrix"`
		Constants []float64   `json:"constants"`
	}
	decode(raw, &in)
	m, n := len(in.Matrix), len(in.Matrix[0])
	if len(in.Constants) != m {
		return nil, invalid("arguments.constants", "one constant is required per matrix row")
	}
	a := make([][]*big.Rat, m)
	for i, row := range in.Matrix {
		if len(row) != n {
			return nil, invalid("arguments.matrix", "all rows must have the same number of columns")
		}
		a[i] = make([]*big.Rat, n+1)
		for j, x := range append(append([]float64(nil), row...), in.Constants[i]) {
			a[i][j] = big.NewRat(int64(x), 1)
		}
	}
	pivots := make([]int, 0, n)
	r := 0
	for col := 0; col < n && r < m; col++ {
		pivot := r
		for pivot < m && a[pivot][col].Sign() == 0 {
			pivot++
		}
		if pivot == m {
			continue
		}
		a[r], a[pivot] = a[pivot], a[r]
		divisor := new(big.Rat).Set(a[r][col])
		for j := 0; j <= n; j++ {
			a[r][j].Quo(a[r][j], divisor)
		}
		for i := 0; i < m; i++ {
			if i == r {
				continue
			}
			factor := new(big.Rat).Set(a[i][col])
			for j := 0; j <= n; j++ {
				a[i][j].Sub(a[i][j], new(big.Rat).Mul(factor, a[r][j]))
			}
		}
		pivots = append(pivots, col)
		r++
	}
	consistent := true
	for i := r; i < m; i++ {
		if a[i][n].Sign() != 0 {
			consistent = false
		}
	}
	free := make([]int, 0, n-r)
	for j := 0; j < n; j++ {
		found := false
		for _, p := range pivots {
			if p == j {
				found = true
				break
			}
		}
		if !found {
			free = append(free, j)
		}
	}
	output := make([][]string, m)
	for i, row := range a {
		output[i] = make([]string, n+1)
		for j, x := range row {
			output[i][j] = x.RatString()
		}
	}
	status := "infinite"
	if !consistent {
		status = "inconsistent"
	} else if r == n {
		status = "unique"
	}
	result := map[string]any{"status": status, "consistent": consistent, "rank": r, "pivot_columns": pivots, "free_columns": free, "rref": output, "onto": r == m, "one_to_one": r == n, "column_index_base": 0}
	basis := make([][]string, 0, len(free))
	for _, f := range free {
		v := make([]string, n)
		for j := range v {
			v[j] = "0"
		}
		v[f] = "1"
		for i, p := range pivots {
			v[p] = new(big.Rat).Neg(a[i][f]).RatString()
		}
		basis = append(basis, v)
	}
	result["nullspace_basis"] = basis
	if consistent {
		particular := make([]string, n)
		for j := range particular {
			particular[j] = "0"
		}
		for i, p := range pivots {
			particular[p] = a[i][n].RatString()
		}
		result["particular_solution"] = particular
	}
	if status == "unique" {
		solution := make([]string, n)
		for i, p := range pivots {
			solution[p] = a[i][n].RatString()
		}
		result["solution"] = solution
	}
	return result, nil
}

func combinatorics(raw json.RawMessage) (any, error) {
	var in struct {
		Operation string
		N         float64
		K         *float64
	}
	decode(raw, &in)
	n := int64(in.N)
	result := big.NewInt(1)
	if in.Operation == "factorial" {
		if in.K != nil {
			return nil, invalid("arguments.k", "omit k for factorial")
		}
		if n > 0 {
			result.MulRange(1, n)
		}
	} else {
		if in.K == nil || *in.K > in.N {
			return nil, invalid("arguments.k", "supply k with 0 <= k <= n")
		}
		k := int64(*in.K)
		if in.Operation == "combinations" {
			result.Binomial(n, k)
		} else if k > 0 {
			result.MulRange(n-k+1, n)
		}
	}
	return map[string]any{"operation": in.Operation, "value": result.String(), "representation": "exact_decimal_string"}, nil
}

func numberTheory(raw json.RawMessage) (any, error) {
	var in struct {
		Operation string
		A         float64
		B         *float64
	}
	decode(raw, &in)
	a := int64(in.A)
	if in.Operation == "factorize" {
		if in.B != nil || a < 2 {
			return nil, invalid("arguments", "factorization requires a >= 2 and no b")
		}
		factors := make([]map[string]int64, 0, 12)
		remaining := a
		for p := int64(2); p*p <= remaining; p++ {
			count := int64(0)
			for remaining%p == 0 {
				count++
				remaining /= p
			}
			if count > 0 {
				factors = append(factors, map[string]int64{"prime": p, "exponent": count})
			}
		}
		if remaining > 1 {
			factors = append(factors, map[string]int64{"prime": remaining, "exponent": 1})
		}
		return map[string]any{"value": a, "factors": factors}, nil
	}
	if in.B == nil {
		return nil, invalid("arguments.b", "b is required for gcd and lcm")
	}
	b := int64(*in.B)
	g := new(big.Int).GCD(nil, nil, big.NewInt(a), big.NewInt(b))
	value := new(big.Int).Set(g)
	if in.Operation == "lcm" {
		value.SetInt64(0)
		if g.Sign() != 0 {
			value.Mul(big.NewInt(a), big.NewInt(b))
			value.Quo(value, g)
		}
	}
	return map[string]any{"operation": in.Operation, "value": value.String(), "representation": "exact_decimal_string"}, nil
}

func convertBase(raw json.RawMessage) (any, error) {
	var in struct{ Value, From, To string }
	decode(raw, &in)
	from, _ := strconv.Atoi(in.From)
	to, _ := strconv.Atoi(in.To)
	digits := strings.TrimPrefix(in.Value, "-")
	if digits == "" {
		return nil, invalid("arguments.value", "value must contain digits")
	}
	for _, r := range digits {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return nil, invalid("arguments.value", "use digits without prefixes, spaces, or separators")
		}
	}
	n, ok := new(big.Int).SetString(in.Value, from)
	if !ok {
		return nil, invalid("arguments.value", "digits are invalid for the input base")
	}
	return map[string]any{"value": n.Text(to), "base": to, "representation": "exact_integer_string"}, nil
}
