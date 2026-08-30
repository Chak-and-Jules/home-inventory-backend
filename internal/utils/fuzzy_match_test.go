package utils_test

import (
	"strings"
	"testing"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestLevenshteinDistance(t *testing.T) {
	assert.Equal(t, 0, utils.LevenshteinDistance("Milk", "milk"))
	assert.Equal(t, 1, utils.LevenshteinDistance("Milk", "Mlk"))
	assert.Equal(t, 3, utils.LevenshteinDistance("Kitten", "Sitting"))
	assert.Equal(t, 4, utils.LevenshteinDistance("", "test"))
}

func TestStringSimilarity(t *testing.T) {
	assert.Equal(t, 1.0, utils.StringSimilarity("Organic Milk", "organic milk"))
	assert.Equal(t, 0.0, utils.StringSimilarity("", "test"))
	assert.True(t, utils.StringSimilarity("Milk", "Whole Milk") > 0.8)
	assert.True(t, utils.StringSimilarity("Apple Juice", "Orange Juice") > 0.4)
}

func TestFindBestMatch(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()

	defs := []models.ItemDefinition{
		{ID: id1, Name: "Organic Whole Milk"},
		{ID: id2, Name: "Fresh Sliced Bread"},
	}

	match1 := utils.FindBestMatch("Organic Milk", defs)
	assert.NotNil(t, match1.MatchedDefinition)
	assert.Equal(t, id1, match1.MatchedDefinition.ID)
	assert.True(t, match1.Confidence >= 0.6)

	match2 := utils.FindBestMatch("Car Engine Oil 5W30", defs)
	assert.Nil(t, match2.MatchedDefinition)
}

func TestStandardReceiptOCRParser(t *testing.T) {
	parser := &utils.StandardReceiptOCRParser{}

	receiptText := `
GROCERY STORE #123
123 MAIN STREET
------------------
2x Organic Milk $4.99
Fresh Sliced Bread $2.49
2x Apple Juice $1.99
SUBTOTAL $11.46
TAX $0.92
TOTAL $12.38
THANK YOU!
`

	items, err := parser.ParseReceipt(strings.NewReader(receiptText))
	assert.NoError(t, err)
	assert.Len(t, items, 3)

	assert.Equal(t, "Organic Milk", items[0].RawName)
	assert.Equal(t, 2.0, items[0].Quantity)
	assert.Equal(t, 4.99, items[0].Price)

	assert.Equal(t, "Fresh Sliced Bread", items[1].RawName)
	assert.Equal(t, 1.0, items[1].Quantity)
	assert.Equal(t, 2.49, items[1].Price)

	assert.Equal(t, "Apple Juice", items[2].RawName)
	assert.Equal(t, 2.0, items[2].Quantity)
	assert.Equal(t, 1.99, items[2].Price)
}
