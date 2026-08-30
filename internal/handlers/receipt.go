package handlers

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ReceiptHandler struct {
	DB        *gorm.DB
	OCRParser utils.ReceiptOCRParser
}

type ConfirmReceiptItem struct {
	RawName              string     `json:"raw_name" binding:"required"`
	Quantity             float64    `json:"quantity" binding:"required"`
	Price                float64    `json:"price"`
	ExpirationDate       *time.Time `json:"expiry_date"`
	ItemDefinitionID     *uuid.UUID `json:"item_definition_id"`
	CreateItemDefinition bool       `json:"create_item_definition"`
	CategoryID           *uuid.UUID `json:"category_id"`
	SizeUnitID           *uuid.UUID `json:"size_unit_id"`
}

type ConfirmReceiptJobRequest struct {
	Items []ConfirmReceiptItem `json:"items" binding:"required"`
}

type ReceiptJobItemResponse struct {
	ID                      uuid.UUID              `json:"id"`
	RawName                 string                 `json:"raw_name"`
	Quantity                float64                `json:"quantity"`
	Price                   float64                `json:"price"`
	MatchedItemDefinition   *models.ItemDefinition `json:"matched_item_definition,omitempty"`
	MatchedItemDefinitionID *uuid.UUID             `json:"matched_item_definition_id,omitempty"`
	Confidence              float64                `json:"confidence"`
}

type ReceiptJobResponse struct {
	ID           uuid.UUID                `json:"id"`
	HomeID       uuid.UUID                `json:"home_id"`
	UserID       uuid.UUID                `json:"user_id"`
	Status       string                   `json:"status"`
	ErrorMessage string                   `json:"error_message,omitempty"`
	ImageURL     string                   `json:"image_url,omitempty"`
	Items        []ReceiptJobItemResponse `json:"items"`
	CreatedAt    time.Time                `json:"created_at"`
	UpdatedAt    time.Time                `json:"updated_at"`
}

func (h *ReceiptHandler) getParser() utils.ReceiptOCRParser {
	if h.OCRParser != nil {
		return h.OCRParser
	}
	return &utils.StandardReceiptOCRParser{}
}

func (h *ReceiptHandler) ScanReceipt(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	userIDVal, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid user ID in token")})
		return
	}
	userID := userIDVal.(uuid.UUID)

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Receipt image or document is required")})
		return
	}
	defer file.Close()

	// Validate file type
	filename := strings.ToLower(header.Filename)
	contentType := header.Header.Get("Content-Type")
	if !strings.HasSuffix(filename, ".jpg") && !strings.HasSuffix(filename, ".jpeg") &&
		!strings.HasSuffix(filename, ".png") && !strings.HasSuffix(filename, ".pdf") &&
		!strings.Contains(contentType, "image/jpeg") && !strings.Contains(contentType, "image/png") &&
		!strings.Contains(contentType, "application/pdf") && !strings.Contains(contentType, "text/plain") {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Unsupported file format. Please upload JPEG, PNG, or PDF")})
		return
	}

	// Read content buffer
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to read receipt file")})
		return
	}

	job := models.ReceiptJob{
		HomeID:    homeID,
		UserID:    userID,
		Status:    "processing",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := h.DB.Create(&job).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to create receipt processing job")})
		return
	}

	// Process OCR asynchronously
	go h.processReceiptAsync(job.ID, homeID, fileBytes)

	c.JSON(http.StatusAccepted, gin.H{
		"message": i18n.TranslateDB(h.DB, c, "Receipt uploaded successfully and is being processed"),
		"job_id":  job.ID,
		"status":  "processing",
	})
}

func (h *ReceiptHandler) processReceiptAsync(jobID, homeID uuid.UUID, fileBytes []byte) {
	parser := h.getParser()
	extractedItems, err := parser.ParseReceipt(bytes.NewReader(fileBytes))
	if err != nil {
		h.DB.Model(&models.ReceiptJob{}).Where("id = ?", jobID).Updates(map[string]interface{}{
			"status":        "failed",
			"error_message": fmt.Sprintf("OCR parsing failed: %v", err),
			"updated_at":    time.Now(),
		})
		return
	}

	// Fetch existing ItemDefinitions for fuzzy matching
	var itemDefs []models.ItemDefinition
	h.DB.Where("home_id = ?", homeID).Find(&itemDefs)

	var jobItems []models.ReceiptJobItem
	for _, extItem := range extractedItems {
		match := utils.FindBestMatch(extItem.RawName, itemDefs)

		var matchedID *uuid.UUID
		if match.MatchedDefinition != nil {
			matchedID = &match.MatchedDefinition.ID
		}

		jobItems = append(jobItems, models.ReceiptJobItem{
			ReceiptJobID:            jobID,
			RawName:                 extItem.RawName,
			Quantity:                extItem.Quantity,
			Price:                   extItem.Price,
			MatchedItemDefinitionID: matchedID,
			Confidence:              match.Confidence,
			CreatedAt:               time.Now(),
		})
	}

	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if len(jobItems) > 0 {
			if err := tx.Create(&jobItems).Error; err != nil {
				return err
			}
		}
		return tx.Model(&models.ReceiptJob{}).Where("id = ?", jobID).Updates(map[string]interface{}{
			"status":     "completed",
			"updated_at": time.Now(),
		}).Error
	})

	if err != nil {
		h.DB.Model(&models.ReceiptJob{}).Where("id = ?", jobID).Updates(map[string]interface{}{
			"status":        "failed",
			"error_message": fmt.Sprintf("Failed to save OCR results: %v", err),
			"updated_at":    time.Now(),
		})
	}
}

func (h *ReceiptHandler) GetReceiptJob(c *gin.Context) {
	jobID, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid receipt job ID")
	if !ok {
		return
	}

	var job models.ReceiptJob
	if err := h.DB.Preload("Items.MatchedItemDefinition").First(&job, jobID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Receipt job not found")})
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, job.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Access denied to this home")})
		return
	}

	var itemResponses []ReceiptJobItemResponse
	for _, item := range job.Items {
		itemResponses = append(itemResponses, ReceiptJobItemResponse{
			ID:                      item.ID,
			RawName:                 item.RawName,
			Quantity:                item.Quantity,
			Price:                   item.Price,
			MatchedItemDefinition:   item.MatchedItemDefinition,
			MatchedItemDefinitionID: item.MatchedItemDefinitionID,
			Confidence:              item.Confidence,
		})
	}

	resp := ReceiptJobResponse{
		ID:           job.ID,
		HomeID:       job.HomeID,
		UserID:       job.UserID,
		Status:       job.Status,
		ErrorMessage: job.ErrorMessage,
		ImageURL:     job.ImageURL,
		Items:        itemResponses,
		CreatedAt:    job.CreatedAt,
		UpdatedAt:    job.UpdatedAt,
	}

	c.JSON(http.StatusOK, resp)
}

func (h *ReceiptHandler) ConfirmReceiptJob(c *gin.Context) {
	jobID, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid receipt job ID")
	if !ok {
		return
	}

	var job models.ReceiptJob
	if err := h.DB.First(&job, jobID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Receipt job not found")})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, job.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req ConfirmReceiptJobRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	var createdInventoryItems []models.InventoryItem

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		for _, confirmItem := range req.Items {
			if confirmItem.Quantity <= 0 {
				return fmt.Errorf("invalid quantity for item: %s", confirmItem.RawName)
			}

			var targetDefID uuid.UUID

			if confirmItem.ItemDefinitionID != nil && *confirmItem.ItemDefinitionID != uuid.Nil {
				// Verify definition exists in home
				var def models.ItemDefinition
				if err := tx.Where("id = ? AND home_id = ?", *confirmItem.ItemDefinitionID, job.HomeID).First(&def).Error; err != nil {
					return fmt.Errorf("item definition not found or does not belong to home")
				}
				targetDefID = def.ID
			} else {
				// Create new item definition if required or no definition ID supplied
				newDef := models.ItemDefinition{
					HomeID:     job.HomeID,
					Name:       confirmItem.RawName,
					CategoryID: confirmItem.CategoryID,
					SizeUnitID: confirmItem.SizeUnitID,
				}
				if err := tx.Create(&newDef).Error; err != nil {
					return fmt.Errorf("failed to create item definition: %w", err)
				}
				targetDefID = newDef.ID
			}

			// Create InventoryItem
			invItem := models.InventoryItem{
				HomeID:           job.HomeID,
				ItemDefinitionID: targetDefID,
				Quantity:         confirmItem.Quantity,
				ExpirationDate:   confirmItem.ExpirationDate,
			}
			if err := tx.Create(&invItem).Error; err != nil {
				return fmt.Errorf("failed to create inventory item: %w", err)
			}

			// Create InventoryTransaction
			txLog := models.InventoryTransaction{
				HomeID:           job.HomeID,
				ItemDefinitionID: targetDefID,
				InventoryItemID:  invItem.ID,
				QuantityChange:   confirmItem.Quantity,
			}
			if err := tx.Create(&txLog).Error; err != nil {
				return fmt.Errorf("failed to create inventory transaction: %w", err)
			}

			if err := utils.UpdateShoppingListForDefinition(tx, job.HomeID, targetDefID); err != nil {
				return err
			}

			createdInventoryItems = append(createdInventoryItems, invItem)
		}

		return nil
	})

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to confirm receipt items: "+err.Error())})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": i18n.TranslateDB(h.DB, c, "Receipt items confirmed and added to inventory successfully"),
		"count":   len(createdInventoryItems),
	})
}
