package utils_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestReceiptParserItemFormats(t *testing.T) {
	tests := []struct {
		name, input, product string
		quantity, price      float64
	}{
		{"plain", "  Milk 4.99  ", "Milk", 1, 4.99},
		{"decimal comma", "Bread 2,49 EUR", "Bread", 1, 2.49},
		{"prefix quantity", "2X Eggs 8.00", "Eggs", 2, 8},
		{"fractional quantity", "1,5 x Apples 6,00", "Apples", 1.5, 6},
		{"unit price", "Apples 3 @ 2.00 6.00", "Apples", 3, 6},
		{"asterisk quantity", "2 * Rice 10.00", "Rice", 2, 10},
		{"trailing quantity", "Milk 2 9.98", "Milk", 2, 9.98},
		{"trailing fractional quantity", "Apples @ 1,5 6.00", "Apples", 1.5, 6},
		{"tax marker", "Milk 4.99*", "Milk", 1, 4.99},
		{"zero quantity retained as name", "0x Milk 4.99", "0x Milk", 1, 4.99},
		{"zero trailing quantity retained as name", "Milk 0 4.99", "Milk 0", 1, 4.99},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := (&utils.StandardReceiptOCRParser{}).ParseReceipt(strings.NewReader(tt.input))
			require.NoError(t, err)
			require.Equal(t, []utils.ExtractedReceiptItem{{RawName: tt.product, Quantity: tt.quantity, Price: tt.price}}, items)
		})
	}
}

func TestReceiptParserSkipsNonItems(t *testing.T) {
	lines := []string{
		"", "   ", "Milk", "Milk 0.00", "4.99", "2x 4.99",
		"Milk " + strings.Repeat("9", 400) + ".99",
		"TOTAL 4.99", "SUBTOTAL 4.99", "TOPLAM 4.99", "ARA TOPLAM 4.99",
		"TAX 4.99", "VAT 4.99", "KDV 4.99", "CASH 4.99", "CHANGE 4.99",
		"PARA USTU 4.99", "VISA 4.99", "MASTERCARD 4.99", "CARD 4.99",
		"KREDI KARTI 4.99", "THANK YOU 4.99", "TESEKKUR 4.99", "IYI GUNLER 4.99",
		"RECEIPT 4.99", "STORE 4.99", "FIS NO 4.99", "FATURA 4.99",
		"TARIH 4.99", "SAAT 4.99", "DISCOUNT 4.99", "SAVINGS 4.99",
		"INDIRIM 4.99", "TEL: 4.99", "PHONE: 4.99",
	}
	for _, line := range lines {
		t.Run(line, func(t *testing.T) {
			items, err := (&utils.StandardReceiptOCRParser{}).ParseReceipt(strings.NewReader(line + "\nBread 2.00"))
			require.NoError(t, err)
			require.Equal(t, []utils.ExtractedReceiptItem{{RawName: "Bread", Quantity: 1, Price: 2}}, items)
		})
	}
}

type failedReceiptReader struct{ err error }

func (r failedReceiptReader) Read([]byte) (int, error) { return 0, r.err }

func TestReceiptParserReadFailure(t *testing.T) {
	wantErr := errors.New("receipt reader unavailable")
	reader := io.MultiReader(strings.NewReader("Milk 4.99\n"), failedReceiptReader{wantErr})
	items, err := (&utils.StandardReceiptOCRParser{}).ParseReceipt(reader)
	require.ErrorIs(t, err, wantErr)
	require.ErrorContains(t, err, "failed scanning receipt")
	require.Nil(t, items, "partial receipts must not be returned as successful results")
}

func TestReceiptParserOversizedLine(t *testing.T) {
	items, err := (&utils.StandardReceiptOCRParser{}).ParseReceipt(strings.NewReader(strings.Repeat("a", 70*1024)))
	require.ErrorContains(t, err, "failed scanning receipt")
	require.Nil(t, items)
}

func TestReceiptParserEmptyInput(t *testing.T) {
	items, err := (&utils.StandardReceiptOCRParser{}).ParseReceipt(strings.NewReader(""))
	require.NoError(t, err)
	require.Empty(t, items)
}
