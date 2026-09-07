package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ProcessPredictions calculates and updates predictions for all inventory items across all homes.
func ProcessPredictions(db *gorm.DB) error {
	now := time.Now()

	// 1. Delete predictions older than 6 months
	sixMonthsAgo := now.AddDate(0, -6, 0)
	if err := db.Where("created_at < ?", sixMonthsAgo).Delete(&models.InventoryPrediction{}).Error; err != nil {
		return fmt.Errorf("failed to purge old predictions: %w", err)
	}

	// 2. Fetch all inventory items with item definition and home
	var items []models.InventoryItem
	if err := db.Find(&items).Error; err != nil {
		return fmt.Errorf("failed to fetch inventory items: %w", err)
	}

	thirtyDaysAgo := now.AddDate(0, 0, -30)
	sixtyDaysAgo := now.AddDate(0, 0, -60)
	yesterday := now.Add(-24 * time.Hour)
	sevenDaysAgo := now.AddDate(0, 0, -7)

	for _, item := range items {
		// Get transactions for this item in the last 6 months
		var txs []models.InventoryTransaction
		if err := db.Where("inventory_item_id = ? AND created_at >= ?", item.ID, sixMonthsAgo).
			Order("created_at desc").
			Find(&txs).Error; err != nil {
			log.Printf("Error fetching transactions for item %s: %v", item.ID, err)
			continue
		}

		// Check last update time (either from transactions or item.UpdatedAt)
		lastUpdate := item.UpdatedAt
		for _, tx := range txs {
			if tx.CreatedAt.After(lastUpdate) {
				lastUpdate = tx.CreatedAt
			}
		}

		// Requirement: If an item has not been updated in the last 6 months, ignore completely.
		if lastUpdate.Before(sixMonthsAgo) {
			continue
		}

		// Calculate update frequency
		// Quantity updates in last 30 days & 60 days
		updates30 := 0
		updates60 := 0
		for _, tx := range txs {
			if tx.CreatedAt.After(thirtyDaysAgo) {
				updates30++
			}
			if tx.CreatedAt.After(sixtyDaysAgo) {
				updates60++
			}
		}

		isVeryFrequent := updates30 >= 4
		isModeratelyFrequent := updates60 >= 2

		// Fetch recent predictions for this item
		var recentPredictions []models.InventoryPrediction
		if err := db.Where("inventory_item_id = ? AND created_at >= ?", item.ID, sevenDaysAgo).
			Order("created_at desc").
			Find(&recentPredictions).Error; err != nil {
			log.Printf("Error fetching recent predictions for item %s: %v", item.ID, err)
			continue
		}

		// Rotation checks:
		// - If not very frequent, skip if included in previous day's prediction.
		// - If not moderately frequent, skip if included in last 7 days' predictions.
		if !isVeryFrequent {
			includedYesterday := false
			for _, p := range recentPredictions {
				if p.CreatedAt.After(yesterday) {
					includedYesterday = true
					break
				}
			}
			if includedYesterday {
				continue
			}
		}

		if !isModeratelyFrequent {
			includedIn7Days := len(recentPredictions) > 0
			if includedIn7Days {
				continue
			}
		}

		// Calculation of consumption rate & predicted consumed amount based on negative transactions
		var negTxs []models.InventoryTransaction
		var maxPosChange float64 = 0

		for _, tx := range txs {
			if tx.QuantityChange < 0 {
				negTxs = append(negTxs, tx)
			} else if tx.QuantityChange > maxPosChange {
				maxPosChange = tx.QuantityChange
			}
		}

		if len(negTxs) == 0 {
			// No consumption transactions to base prediction on
			continue
		}

		totalConsumed := 0.0
		for _, tx := range negTxs {
			totalConsumed += math.Abs(tx.QuantityChange)
		}
		avgConsumedPerTx := totalConsumed / float64(len(negTxs))

		// Estimate predicted amount for daily run
		predictedAmount := avgConsumedPerTx

		// Sanity Check 3.1: Check if prediction is within 10% distance from avg quantity change in last 6 months
		// (e.g. math.Abs(predicted - avg) <= 0.10 * avg).
		// Since predictedAmount is initialized to avgConsumedPerTx, it inherently satisfies this condition unless further adjusted.
		if avgConsumedPerTx > 0 && math.Abs(predictedAmount-avgConsumedPerTx) > 0.10*avgConsumedPerTx {
			// Adjust to bounded 10% range if necessary
			if predictedAmount > avgConsumedPerTx*1.10 {
				predictedAmount = avgConsumedPerTx * 1.10
			} else if predictedAmount < avgConsumedPerTx*0.90 {
				predictedAmount = avgConsumedPerTx * 0.90
			}
		}

		// Max Cap 3.2: 125% of the maximum single positive quantity change in the last 6 months
		if maxPosChange > 0 {
			maxCap := maxPosChange * 1.25
			if predictedAmount > maxCap {
				predictedAmount = maxCap
			}
		}

		// Perform database update for this inventory_item_id inside transaction:
		// Delete previous predictions for this item, then save new prediction.
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("inventory_item_id = ?", item.ID).Delete(&models.InventoryPrediction{}).Error; err != nil {
				return err
			}

			prediction := models.InventoryPrediction{
				ID:                      uuid.New(),
				InventoryItemID:         item.ID,
				PredictedConsumedAmount: predictedAmount,
				Status:                  models.PredictionStatusPredicted,
				CreatedAt:               now,
				UpdatedAt:               now,
			}
			return tx.Create(&prediction).Error
		})

		if err != nil {
			log.Printf("Failed to update prediction for item %s: %v", item.ID, err)
		}
	}

	return nil
}

// Handler is the http.HandlerFunc standard Cloud Run Function entry point.
func Handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		http.Error(w, "DATABASE_URL not configured", http.StatusInternalServerError)
		return
	}

	db, err := gorm.Open(postgres.Open(dbURL), &gorm.Config{})
	if err != nil {
		http.Error(w, fmt.Sprintf("Database connection error: %v", err), http.StatusInternalServerError)
		return
	}

	if err := ProcessPredictions(db); err != nil {
		http.Error(w, fmt.Sprintf("Error processing predictions: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/", Handler)
	log.Printf("Predictions Cloud Run function listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
