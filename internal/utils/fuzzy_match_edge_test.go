package utils_test

import (
	"testing"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestMatchingSimilarityEdges(t *testing.T) {
	tests := []struct {
		left, right string
		want        float64
	}{
		{"milk", "", 0},
		{"   ", "milk", 0},
		{"milk", "milky", 0.96},
		{"milky", "milk", 0.96},
		{"abcde", "abxyz", 0.4},
		{"abxyz", "abcde", 0.4},
		{"red apple", "green apple", 0.85},
		{"apple", "fresh red apple", 0.95},
		{"fresh red apple", "apple", 0.95},
		{"milk organic", "organic milk", 0.95},
		{" Whole-Milk_2/L., ", "whole milk 2 l", 1},
		{"cat", "dog", 0},
	}
	for _, tt := range tests {
		t.Run(tt.left+"/"+tt.right, func(t *testing.T) {
			require.InDelta(t, tt.want, utils.StringSimilarity(tt.left, tt.right), 1e-12)
		})
	}
}

func TestLevenshteinUnicodeAndEmpty(t *testing.T) {
	for _, tt := range []struct {
		left, right string
		want        int
	}{
		{"", "", 0}, {"abc", "", 3}, {"猫", "犬", 1}, {"猫猫", "猫", 1},
		{"", "猫猫", 2}, {" abc ", "ABC", 0},
	} {
		t.Run(tt.left+"/"+tt.right, func(t *testing.T) {
			require.Equal(t, tt.want, utils.LevenshteinDistance(tt.left, tt.right))
		})
	}
}

func TestFindBestMatchThresholdAndTies(t *testing.T) {
	t.Run("no definitions", func(t *testing.T) {
		require.Equal(t, utils.MatchResult{}, utils.FindBestMatch("milk", nil))
	})
	t.Run("empty input", func(t *testing.T) {
		require.Equal(t, utils.MatchResult{}, utils.FindBestMatch("", []models.ItemDefinition{{Name: "milk"}}))
	})
	t.Run("threshold inclusive and stable tie", func(t *testing.T) {
		defs := []models.ItemDefinition{{Name: "abxyz"}, {Name: "abcxy"}, {Name: "abczz"}}
		match := utils.FindBestMatch("abcde", defs)
		require.Same(t, &defs[1], match.MatchedDefinition)
		require.InDelta(t, 0.6, match.Confidence, 1e-12)
	})
	t.Run("rejected match retains confidence", func(t *testing.T) {
		match := utils.FindBestMatch("abcde", []models.ItemDefinition{{Name: "abxyz"}})
		require.Nil(t, match.MatchedDefinition)
		require.InDelta(t, 0.4, match.Confidence, 1e-12)
	})
}
