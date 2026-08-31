package utils

import (
	"sort"
	"strings"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
)

// normalizeString replaces Turkish/accented characters with standard ASCII equivalents and removes punctuation
func normalizeString(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	replacer := strings.NewReplacer(
		"ş", "s", "ı", "i", "ğ", "g", "ü", "u", "ö", "o", "ç", "c",
		"İ", "i", "I", "i", "Ş", "s", "Ğ", "g", "Ü", "u", "Ö", "o", "Ç", "c",
		"-", " ", "_", " ", "/", " ", ".", "", ",", "",
	)
	return replacer.Replace(s)
}

// LevenshteinDistance calculates the edit distance between two strings
func LevenshteinDistance(s1, s2 string) int {
	s1 = normalizeString(s1)
	s2 = normalizeString(s2)

	r1, r2 := []rune(s1), []rune(s2)
	len1, len2 := len(r1), len(r2)

	if len1 == 0 {
		return len2
	}
	if len2 == 0 {
		return len1
	}

	column := make([]int, len1+1)
	for i := 0; i <= len1; i++ {
		column[i] = i
	}

	for j := 1; j <= len2; j++ {
		column[0] = j
		lastDiagonal := j - 1
		for i := 1; i <= len1; i++ {
			oldColumn := column[i]
			cost := 0
			if r1[i-1] != r2[j-1] {
				cost = 1
			}
			column[i] = min(column[i]+1, column[i-1]+1, lastDiagonal+cost)
			lastDiagonal = oldColumn
		}
	}

	return column[len1]
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// StringSimilarity returns a confidence score between 0.0 and 1.0 based on Levenshtein distance, character normalization, and token-set matching
func StringSimilarity(s1, s2 string) float64 {
	str1 := normalizeString(s1)
	str2 := normalizeString(s2)

	if str1 == "" || str2 == "" {
		return 0.0
	}

	if str1 == str2 {
		return 1.0
	}

	tokens1 := strings.Fields(str1)
	tokens2 := strings.Fields(str2)

	// Token set matching (order-independent matching)
	sorted1 := make([]string, len(tokens1))
	copy(sorted1, tokens1)
	sort.Strings(sorted1)

	sorted2 := make([]string, len(tokens2))
	copy(sorted2, tokens2)
	sort.Strings(sorted2)

	if strings.Join(sorted1, " ") == strings.Join(sorted2, " ") {
		return 0.95
	}

	// Calculate token overlap
	set1 := make(map[string]bool)
	for _, t := range tokens1 {
		set1[t] = true
	}
	commonCount := 0
	for _, t := range tokens2 {
		if set1[t] {
			commonCount++
		}
	}

	overlapRatio := 0.0
	if len(tokens1) > 0 && len(tokens2) > 0 {
		minTokens := len(tokens1)
		if len(tokens2) < minTokens {
			minTokens = len(tokens2)
		}
		overlapRatio = float64(commonCount) / float64(minTokens)
	}

	if overlapRatio >= 0.5 {
		tokenScore := 0.75 + (0.2 * overlapRatio)
		return tokenScore
	}

	// Substring / token containment check
	if strings.Contains(str1, str2) || strings.Contains(str2, str1) {
		minLen := float64(len(str1))
		if len(str2) < len(str1) {
			minLen = float64(len(str2))
		}
		maxLen := float64(len(str1))
		if len(str2) > len(str1) {
			maxLen = float64(len(str2))
		}
		return 0.8 + 0.2*(minLen/maxLen)
	}

	dist := LevenshteinDistance(str1, str2)
	maxLen := float64(len([]rune(str1)))
	if float64(len([]rune(str2))) > maxLen {
		maxLen = float64(len([]rune(str2)))
	}

	score := 1.0 - (float64(dist) / maxLen)
	if score < 0 {
		return 0
	}
	return score
}

type MatchResult struct {
	MatchedDefinition *models.ItemDefinition
	Confidence        float64
}

// FindBestMatch matches raw extracted item name against a slice of ItemDefinitions
func FindBestMatch(rawName string, itemDefs []models.ItemDefinition) MatchResult {
	var bestMatch *models.ItemDefinition
	bestScore := 0.0

	for i := range itemDefs {
		score := StringSimilarity(rawName, itemDefs[i].Name)
		if score > bestScore {
			bestScore = score
			bestMatch = &itemDefs[i]
		}
	}

	// Only return as matched if confidence meets 0.6 threshold
	if bestScore >= 0.6 {
		return MatchResult{
			MatchedDefinition: bestMatch,
			Confidence:        bestScore,
		}
	}

	return MatchResult{
		MatchedDefinition: nil,
		Confidence:        bestScore,
	}
}
