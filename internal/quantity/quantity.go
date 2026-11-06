// Package quantity isolates quantity arithmetic from binary floating point.
// float64 is retained at the existing JSON/ORM boundary; calculations use decimal
// rationals, and persisted amounts are limited to six fractional base-unit digits.
package quantity

import (
	"errors"
	"math"
	"math/big"
	"strconv"
)

const Max = 1_000_000_000

var ErrInvalid = errors.New("Invalid quantity precision or range")

func rational(v float64) *big.Rat {
	r, ok := new(big.Rat).SetString(strconv.FormatFloat(v, 'f', -1, 64))
	if !ok {
		panic("non-finite quantity")
	}
	return r
}

func Validate(v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > Max {
		return ErrInvalid
	}
	r := new(big.Rat).Mul(rational(v), big.NewRat(1_000_000, 1))
	if !r.IsInt() {
		return ErrInvalid
	}
	return nil
}

func ToBase(v, factor float64) (float64, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) || factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0) {
		return 0, ErrInvalid
	}
	return ToBaseText(strconv.FormatFloat(v, 'f', -1, 64), factor)
}

// ToBaseText validates the exact JSON decimal before crossing the float64 ORM
// boundary, including precision that JSON decoding into float64 would discard.
func ToBaseText(text string, factor float64) (float64, error) {
	v, ok := new(big.Rat).SetString(text)
	if !ok || factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0) {
		return 0, ErrInvalid
	}
	r := new(big.Rat).Mul(v, rational(factor))
	if new(big.Rat).Abs(r).Cmp(big.NewRat(Max, 1)) > 0 || !new(big.Rat).Mul(r, big.NewRat(1_000_000, 1)).IsInt() {
		return 0, ErrInvalid
	}
	f, _ := r.Float64()
	return f, nil
}

func FromBase(v, factor float64) float64 {
	f, _ := new(big.Rat).Quo(rational(v), rational(factor)).Float64()
	return f
}

func Add(a, b float64) float64 {
	f, _ := new(big.Rat).Add(rational(a), rational(b)).Float64()
	return f
}
func Sub(a, b float64) float64 {
	f, _ := new(big.Rat).Sub(rational(a), rational(b)).Float64()
	return f
}
