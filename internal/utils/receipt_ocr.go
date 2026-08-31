package utils

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

type ExtractedReceiptItem struct {
	RawName  string  `json:"raw_name"`
	Quantity float64 `json:"quantity"`
	Price    float64 `json:"price"`
}

type ReceiptOCRParser interface {
	ParseReceipt(r io.Reader) ([]ExtractedReceiptItem, error)
}

type StandardReceiptOCRParser struct{}

var (
	// Matches prices with optional currency symbols ($, €, £, ₺, TL, TRY, USD, EUR) and either dot or comma decimal: e.g. "$4.99", "4,99 TL", "12.50*", "%18 4.99"
	priceRegex = regexp.MustCompile(`(?:[\$€£₺]|\b(?:TL|TRY|USD|EUR)\b)?\s*(\d+[.,]\d{2})\s*(?:[\$€£₺]|\b(?:TL|TRY|USD|EUR)\b)?\s*[*%A-Za-z]?\s*$`)

	// Quantity patterns e.g. "3 x 12.50" or "3x" or "3 *" or "3 @"
	qtyWithUnitPriceRegex = regexp.MustCompile(`(\d+(?:[.,]\d+)?)\s*[xX@*]\s*(\d+(?:[.,]\d+)?)?`)
	prefixQtyRegex        = regexp.MustCompile(`^(\d+(?:[.,]\d+)?)\s*[xX@*]\s*`)
	trailingQtyRegex      = regexp.MustCompile(`\s+@?\s*(\d+(?:[.,]\d+)?)$`)
)

func (p *StandardReceiptOCRParser) ParseReceipt(r io.Reader) ([]ExtractedReceiptItem, error) {
	scanner := bufio.NewScanner(r)
	var items []ExtractedReceiptItem

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		lower := strings.ToLower(line)
		// Skip non-item lines (headers, footers, tax summaries, totals, payment details)
		if strings.Contains(lower, "total") || strings.Contains(lower, "subtotal") ||
			strings.Contains(lower, "toplam") || strings.Contains(lower, "ara toplam") ||
			strings.Contains(lower, "tax") || strings.Contains(lower, "vat") || strings.Contains(lower, "kdv") ||
			strings.Contains(lower, "cash") || strings.Contains(lower, "change") || strings.Contains(lower, "para ustu") ||
			strings.Contains(lower, "visa") || strings.Contains(lower, "mastercard") || strings.Contains(lower, "card") || strings.Contains(lower, "kredi karti") ||
			strings.Contains(lower, "thank you") || strings.Contains(lower, "tesekkur") || strings.Contains(lower, "iyi gunler") ||
			strings.Contains(lower, "receipt") || strings.Contains(lower, "store") || strings.Contains(lower, "fis no") || strings.Contains(lower, "fatura") ||
			strings.Contains(lower, "tarih") || strings.Contains(lower, "saat") || strings.Contains(lower, "discount") || strings.Contains(lower, "savings") ||
			strings.Contains(lower, "indirim") || strings.Contains(lower, "tel:") || strings.Contains(lower, "phone:") {
			continue
		}

		// Extract price at end of line
		priceMatches := priceRegex.FindStringSubmatch(line)
		if len(priceMatches) < 2 {
			continue
		}

		rawPriceStr := strings.Replace(priceMatches[1], ",", ".", 1)
		priceVal, err := strconv.ParseFloat(rawPriceStr, 64)
		if err != nil || priceVal <= 0 {
			continue
		}

		// Strip price part from line to extract product name & quantity
		lineNoPrice := strings.TrimSpace(line[:strings.LastIndex(line, priceMatches[0])])
		if lineNoPrice == "" {
			continue
		}

		quantity := 1.0

		// Check for quantity in line item e.g. "3 x 1.50" or "2x Milk" or "Milk 2"
		if qtyMatch := qtyWithUnitPriceRegex.FindStringSubmatch(lineNoPrice); len(qtyMatch) > 1 {
			qStr := strings.Replace(qtyMatch[1], ",", ".", 1)
			if q, err := strconv.ParseFloat(qStr, 64); err == nil && q > 0 {
				quantity = q
				lineNoPrice = strings.TrimSpace(strings.Replace(lineNoPrice, qtyMatch[0], "", 1))
			}
		} else if qtyLoc := prefixQtyRegex.FindStringSubmatch(lineNoPrice); len(qtyLoc) > 1 {
			qStr := strings.Replace(qtyLoc[1], ",", ".", 1)
			if q, err := strconv.ParseFloat(qStr, 64); err == nil && q > 0 {
				quantity = q
				lineNoPrice = strings.TrimSpace(lineNoPrice[len(qtyLoc[0]):])
			}
		} else if qtyLoc := trailingQtyRegex.FindStringSubmatch(lineNoPrice); len(qtyLoc) > 1 {
			qStr := strings.Replace(qtyLoc[1], ",", ".", 1)
			if q, err := strconv.ParseFloat(qStr, 64); err == nil && q > 0 {
				quantity = q
				lineNoPrice = strings.TrimSpace(lineNoPrice[:len(lineNoPrice)-len(qtyLoc[0])])
			}
		}

		// Clean up leading numbers or noise characters remaining in product name
		lineNoPrice = strings.TrimSpace(lineNoPrice)
		if lineNoPrice == "" {
			continue
		}

		items = append(items, ExtractedReceiptItem{
			RawName:  lineNoPrice,
			Quantity: quantity,
			Price:    priceVal,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed scanning receipt: %w", err)
	}

	return items, nil
}
