package quantity

import (
	"math"
	"testing"
)

func TestDecimalConversionAndArithmetic(t *testing.T) {
	for _, tc := range []struct{ value, factor, want float64 }{{3, 1000, 3000}, {0.8, 1000, 800}, {0.000000001, 1000, 0.000001}, {0, 1000, 0}} {
		got, err := ToBase(tc.value, tc.factor)
		if err != nil || got != tc.want {
			t.Fatalf("ToBase(%v,%v) = %v, %v", tc.value, tc.factor, got, err)
		}
	}
	if Add(0.1, 0.2) != 0.3 || Sub(0.3, 0.1) != 0.2 {
		t.Fatal("decimal arithmetic drift")
	}
	v := 800.123456
	for range 1000 {
		x, err := ToBase(FromBase(v, 1000), 1000)
		if err != nil || x != v {
			t.Fatalf("round trip: %v, %v", x, err)
		}
		v = x
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), Max + 1, 0.0000001} {
		if _, err := ToBase(v, 1); err == nil {
			t.Fatalf("accepted %v", v)
		}
	}
}

func TestExactJSONDecimalPrecision(t *testing.T) {
	for _, text := range []string{"0.10000000000000000001", "1000000000.00000001", "0.0000001"} {
		if _, err := ToBaseText(text, 1); err == nil {
			t.Fatalf("accepted excessive exact precision: %s", text)
		}
	}
	v, err := ToBaseText("1e-9", 1000)
	if err != nil || v != 0.000001 {
		t.Fatalf("small kg amount: %v %v", v, err)
	}
}

func TestValidateQuantityBoundaries(t *testing.T) {
	for _, value := range []float64{0, Max, -Max, 0.000001, -0.000001, 800.123456} {
		if err := Validate(value); err != nil {
			t.Errorf("valid amount %v rejected: %v", value, err)
		}
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), Max + 1, -Max - 1, 0.0000001, -0.0000001} {
		if err := Validate(value); err != ErrInvalid {
			t.Errorf("invalid amount %v: got %v", value, err)
		}
	}
	for _, factor := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if _, err := ToBase(1, factor); err != ErrInvalid {
			t.Errorf("invalid factor %v: got %v", factor, err)
		}
		if _, err := ToBaseText("1", factor); err != ErrInvalid {
			t.Errorf("invalid decimal factor %v: got %v", factor, err)
		}
	}
	if _, err := ToBaseText("not-a-decimal", 1); err != ErrInvalid {
		t.Fatalf("malformed decimal: %v", err)
	}
}

func TestArithmeticRejectsNonfiniteOperands(t *testing.T) {
	for _, tc := range []struct {
		name      string
		operation func()
	}{
		{"sum", func() { Add(math.NaN(), 1) }},
		{"difference", func() { Sub(1, math.Inf(1)) }},
		{"display", func() { FromBase(math.Inf(-1), 1000) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if got := recover(); got != "non-finite quantity" {
					t.Fatalf("expected non-finite quantity guard, got %v", got)
				}
			}()
			tc.operation()
		})
	}
}
