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
	// Regex for line items like: "2x Organic Milk 4.99" or "Milk 3.50" or "Bread 2 @ 1.99"
	priceRegex  = regexp.MustCompile(`\$?\s*(\d+\.\d{2})`)
	qtyRegex    = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*[xX@*]\s*`)
	trailingQty = regexp.MustCompile(`\s+@?\s*(\d+(?:\.\d+)?)$`)
)

func (p *StandardReceiptOCRParser) ParseReceipt(r io.Reader) ([]ExtractedReceiptItem, error) {
	scanner := bufio.NewScanner(r)
	var items []ExtractedReceiptItem

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Skip header / footer keywords commonly found on receipts
		lower := strings.ToLower(line)
		if strings.Contains(lower, "total") || strings.Contains(lower, "subtotal") ||
			strings.Contains(lower, "tax") || strings.Contains(lower, "cash") ||
			strings.Contains(lower, "change") || strings.Contains(lower, "visa") ||
			strings.Contains(lower, "mastercard") || strings.Contains(lower, "thank you") ||
			strings.Contains(lower, "receipt") || strings.Contains(lower, "store") {
			continue
		}

		// Find price at end of line
		priceMatches := priceRegex.FindAllStringSubmatch(line, -1)
		if len(priceMatches) == 0 {
			continue
		}

		lastMatch := priceMatches[len(priceMatches)-1]
		priceVal, err := strconv.ParseFloat(lastMatch[1], 64)
		if err != nil || priceVal <= 0 {
			continue
		}

		// Remove price portion from line to isolate name & quantity
		lineNoPrice := strings.TrimSpace(line[:strings.LastIndex(line, lastMatch[0])])
		if lineNoPrice == "" {
			continue
		}

		quantity := 1.0
		// Check prefix quantity e.g. "2x Milk"
		if qtyLoc := qtyRegex.FindStringSubmatch(lineNoPrice); len(qtyLoc) > 1 {
			if q, err := strconv.ParseFloat(qtyLoc[1], 64); err == nil && q > 0 {
				quantity = q
				lineNoPrice = strings.TrimSpace(lineNoPrice[len(qtyLoc[0]):])
			}
		} else if qtyLoc := trailingQty.FindStringSubmatch(lineNoPrice); len(qtyLoc) > 1 { // e.g. "Milk 2"
			if q, err := strconv.ParseFloat(qtyLoc[1], 64); err == nil && q > 0 {
				quantity = q
				lineNoPrice = strings.TrimSpace(lineNoPrice[:len(lineNoPrice)-len(qtyLoc[0])])
			}
		}

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
