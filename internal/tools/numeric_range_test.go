package tools

import (
	"encoding/json"
	"math"
	"math/big"
	"testing"
)

func TestExtremeButFiniteNumericScales(t *testing.T) {
	s := callOK(t, "solve_quadratic", `{"a":1e-200,"b":0,"c":-1e-200}`)
	roots := s["roots"].([]any)
	closeTo(t, roots[0], -1)
	closeTo(t, roots[1], 1)
	s = callOK(t, "solve_quadratic", `{"a":1e-200,"b":0,"c":1e-200}`)
	roots = s["roots"].([]any)
	closeTo(t, roots[0].(map[string]any)["imaginary"], 1)
	s = callOK(t, "linear_regression", `{"points":[[1e-200,3e-200],[2e-200,5e-200],[3e-200,7e-200]]}`)
	closeTo(t, s["slope"], 2)
	s = callOK(t, "linear_regression", `{"points":[[1e-320,4],[2e-320,4]]}`)
	closeTo(t, s["slope"], 0)
	closeTo(t, s["intercept"], 4)
	s = callOK(t, "summarize_numbers", `{"values":[-1e-200,1e-200]}`)
	std := s["population_stddev"].(float64)
	if math.Abs(std/1e-200-1) > 1e-12 {
		t.Fatalf("variance underflowed: %g", std)
	}
	s = callOK(t, "grade_quiz", `{"answers":["ς"],"key":["Σ"]}`)
	closeTo(t, s["percentage"], 100)
}

func FuzzArithmeticExpression(f *testing.F) {
	for _, expression := range []string{"-2^2", "2^-3", "1e-200", "sqrt(49)", "max(1,2)", "sin(pi/2)", "1/0", "(((1)))", ""} {
		f.Add(expression)
	}
	f.Fuzz(func(t *testing.T, expression string) {
		if len(expression) > 512 {
			return
		}
		raw, _ := json.Marshal(map[string]string{"expression": expression})
		result, err := Call("calculate", raw)
		if err == nil {
			if _, err := json.Marshal(result); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func FuzzExactLinearSystem(f *testing.F) {
	f.Add([]byte{2, 2, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	f.Add([]byte{0, 0, 0})
	f.Add([]byte{7, 7, 127, 128, 0, 1, 2, 3, 4})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 2 {
			return
		}
		m, n := int(data[0]%8)+1, int(data[1]%8)+1
		matrix := make([][]int, m)
		constants := make([]int, m)
		at := func(i int) int {
			if i >= len(data) {
				return 0
			}
			return int(int8(data[i]))
		}
		for i := range matrix {
			matrix[i] = make([]int, n)
			for j := range matrix[i] {
				matrix[i][j] = at(2 + i*n + j)
			}
			constants[i] = at(2 + m*n + i)
		}
		knownConsistent := data[0]&128 != 0
		if knownConsistent {
			for i, row := range matrix {
				constants[i] = 0
				for j, a := range row {
					constants[i] += a * at(2+m*n+m+j)
				}
			}
		}
		raw, _ := json.Marshal(map[string]any{"matrix": matrix, "constants": constants})
		result := callOK(t, "solve_linear_system", string(raw))
		if knownConsistent && result["consistent"] != true {
			t.Fatal("known solution was reported inconsistent")
		}
		if int(result["rank"].(float64)) == m && result["consistent"] != true {
			t.Fatal("full row rank system was reported inconsistent")
		}
		verify := func(vector []any, want []int) {
			for i, row := range matrix {
				sum := new(big.Rat)
				for j, a := range row {
					x, ok := new(big.Rat).SetString(vector[j].(string))
					if !ok {
						t.Fatal("invalid rational")
					}
					sum.Add(sum, new(big.Rat).Mul(big.NewRat(int64(a), 1), x))
				}
				if sum.Cmp(big.NewRat(int64(want[i]), 1)) != 0 {
					t.Fatalf("returned vector does not satisfy original row %d", i)
				}
			}
		}
		if result["consistent"] == true {
			verify(result["particular_solution"].([]any), constants)
		}
		for _, vector := range result["nullspace_basis"].([]any) {
			verify(vector.([]any), make([]int, m))
		}
		if len(result["nullspace_basis"].([]any)) != n-int(result["rank"].(float64)) {
			t.Fatal("rank-nullity mismatch")
		}
	})
}
