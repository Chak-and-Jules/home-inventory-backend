package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

type mockOCRParser struct {
	items []utils.ExtractedReceiptItem
	err   error
}

func (m *mockOCRParser) ParseReceipt(r io.Reader) ([]utils.ExtractedReceiptItem, error) {
	return m.items, m.err
}

func setupReceiptTest(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	logger.InitLogger()
	gormDB, mock, err := setupTestDB()
	assert.NoError(t, err)
	return gormDB, mock
}

func TestScanReceipt_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gormDB, mock := setupReceiptTest(t)

	handler := &ReceiptHandler{
		DB: gormDB,
		OCRParser: &mockOCRParser{
			items: []utils.ExtractedReceiptItem{
				{RawName: "Organic Milk", Quantity: 2, Price: 4.99},
			},
		},
	}

	homeID := uuid.New()
	userID := uuid.New()
	i18n.InvalidateUserLanguageCache(userID)

	// 1. Verify access query expectation
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).
			AddRow(userID, homeID, "owner"))

	// 2. ReceiptJob insertion expectation
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "receipt_jobs"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectCommit()

	// Mock async processReceiptAsync query expectations
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE home_id = $1`)).
		WithArgs(homeID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}))

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "receipt_job_items"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "receipt_jobs"`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	// Build multipart request
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "receipt.jpg")
	assert.NoError(t, err)
	_, err = part.Write([]byte("Organic Milk 4.99\n"))
	assert.NoError(t, err)
	writer.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/receipts/scan", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Home-Id", homeID.String())
	c.Request = req
	c.Set("userID", userID)

	handler.ScanReceipt(c)

	assert.Equal(t, http.StatusAccepted, w.Code)
	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "processing", resp["status"])

	time.Sleep(50 * time.Millisecond)
}

func TestGetReceiptJob_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gormDB, mock := setupReceiptTest(t)

	handler := &ReceiptHandler{DB: gormDB}

	jobID := uuid.New()
	homeID := uuid.New()
	userID := uuid.New()
	i18n.InvalidateUserLanguageCache(userID)

	// Mock receipt job query with Preload
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_jobs" WHERE "receipt_jobs"."id" = $1 ORDER BY "receipt_jobs"."id" LIMIT $2`)).
		WithArgs(jobID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "user_id", "status"}).
			AddRow(jobID, homeID, userID, "completed"))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_job_items" WHERE "receipt_job_items"."receipt_job_id" = $1`)).
		WithArgs(jobID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "receipt_job_id", "raw_name", "quantity", "price", "confidence"}).
			AddRow(uuid.New(), jobID, "Organic Milk", 2.0, 4.99, 1.0))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).
			AddRow(userID, homeID, "editor"))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/receipts/jobs/"+jobID.String(), nil)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: jobID.String()}}
	c.Set("userID", userID)

	handler.GetReceiptJob(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp ReceiptJobResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, jobID, resp.ID)
	assert.Equal(t, "completed", resp.Status)
	assert.Len(t, resp.Items, 1)
	assert.Equal(t, "Organic Milk", resp.Items[0].RawName)
}

func TestConfirmReceiptJob_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gormDB, mock := setupReceiptTest(t)

	handler := &ReceiptHandler{DB: gormDB}

	jobID := uuid.New()
	homeID := uuid.New()
	userID := uuid.New()
	existingDefID := uuid.New()
	i18n.InvalidateUserLanguageCache(userID)

	// Job lookup
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_jobs" WHERE "receipt_jobs"."id" = $1 ORDER BY "receipt_jobs"."id" LIMIT $2`)).
		WithArgs(jobID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "user_id", "status"}).
			AddRow(jobID, homeID, userID, "completed"))

	// Verify write access
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).
			AddRow(userID, homeID, "owner"))

	// Transaction:
	mock.ExpectBegin()

	// 1st item: uses existing definition
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE id = $1 AND home_id = $2 ORDER BY "item_definitions"."id" LIMIT $3`)).
		WithArgs(existingDefID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).
			AddRow(existingDefID, homeID, "Organic Milk"))

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "inventory_items"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "inventory_transactions"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

	// Update shopping list check
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
		WithArgs(existingDefID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "low_stock_threshold"}).
			AddRow(existingDefID, homeID, nil))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
		WillReturnError(gorm.ErrRecordNotFound)

	// 2nd item: creates new definition
	newDefID := uuid.New()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "item_definitions"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(newDefID))

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "inventory_items"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "inventory_transactions"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
		WithArgs(newDefID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "low_stock_threshold"}).
			AddRow(newDefID, homeID, nil))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
		WillReturnError(gorm.ErrRecordNotFound)

	mock.ExpectCommit()

	reqPayload := ConfirmReceiptJobRequest{
		Items: []ConfirmReceiptItem{
			{
				RawName:          "Organic Milk",
				Quantity:         2.0,
				Price:            4.99,
				ItemDefinitionID: &existingDefID,
			},
			{
				RawName:              "Fresh Sliced Bread",
				Quantity:             1.0,
				Price:                2.49,
				CreateItemDefinition: true,
			},
		},
	}
	jsonBytes, _ := json.Marshal(reqPayload)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/receipts/jobs/"+jobID.String()+"/confirm", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: jobID.String()}}
	c.Set("userID", userID)

	handler.ConfirmReceiptJob(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, float64(2), resp["count"])
}
