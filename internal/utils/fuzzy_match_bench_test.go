package utils

import (
	"testing"
)

func BenchmarkLevenshteinDistance(b *testing.B) {
	s1 := "Organic Milk (Whole)"
	s2 := "Organic Whole Milk"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		LevenshteinDistance(s1, s2)
	}
}

func BenchmarkLevenshteinDistance_Long(b *testing.B) {
	s1 := "The quick brown fox jumps over the lazy dog."
	s2 := "The quick red fox jumps over the sleeping dog."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		LevenshteinDistance(s1, s2)
	}
}
