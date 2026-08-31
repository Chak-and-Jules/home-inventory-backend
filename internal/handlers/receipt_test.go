package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
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

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "receipt_jobs"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectCommit()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE home_id = $1`)).
		WithArgs(homeID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}))

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "receipt_job_items"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "receipt_jobs"`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

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

func TestScanReceipt_Errors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gormDB, mock := setupReceiptTest(t)
	handler := &ReceiptHandler{DB: gormDB}
	homeID := uuid.New()
	userID := uuid.New()

	t.Run("invalid home ID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/receipts/scan", nil)
		req.Header.Set("X-Home-Id", "invalid-uuid")
		c.Request = req
		c.Set("userID", userID)

		handler.ScanReceipt(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("write access denied", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "viewer"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/receipts/scan", nil)
		req.Header.Set("X-Home-Id", homeID.String())
		c.Request = req
		c.Set("userID", userID)

		handler.ScanReceipt(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("missing user ID", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes"`)).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/receipts/scan", nil)
		req.Header.Set("X-Home-Id", homeID.String())
		c.Request = req

		handler.ScanReceipt(c)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("missing file", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes"`)).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/receipts/scan", nil)
		req.Header.Set("X-Home-Id", homeID.String())
		c.Request = req
		c.Set("userID", userID)

		handler.ScanReceipt(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("unsupported file type", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes"`)).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "document.exe")
		part.Write([]byte("binary content"))
		writer.Close()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/receipts/scan", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("X-Home-Id", homeID.String())
		c.Request = req
		c.Set("userID", userID)

		handler.ScanReceipt(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("DB create job error", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes"`)).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "receipt_jobs"`)).
			WillReturnError(errors.New("db insert error"))
		mock.ExpectRollback()

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "receipt.png")
		part.Write([]byte("PNG image"))
		writer.Close()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/receipts/scan", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("X-Home-Id", homeID.String())
		c.Request = req
		c.Set("userID", userID)

		handler.ScanReceipt(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestProcessReceiptAsync_OCRFailureAndDBFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gormDB, mock := setupReceiptTest(t)
	homeID := uuid.New()
	jobID := uuid.New()

	t.Run("OCR parser error", func(t *testing.T) {
		handler := &ReceiptHandler{
			DB: gormDB,
			OCRParser: &mockOCRParser{
				err: errors.New("OCR failed"),
			},
		}

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "receipt_jobs"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.processReceiptAsync(jobID, homeID, []byte("invalid content"))
	})

	t.Run("DB save items error", func(t *testing.T) {
		handler := &ReceiptHandler{
			DB: gormDB,
			OCRParser: &mockOCRParser{
				items: []utils.ExtractedReceiptItem{{RawName: "Milk", Quantity: 1, Price: 2.50}},
			},
		}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions"`)).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "receipt_job_items"`)).
			WillReturnError(errors.New("insert failed"))
		mock.ExpectRollback()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "receipt_jobs"`)).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.processReceiptAsync(jobID, homeID, []byte("Milk 2.50"))
	})
}

func TestGetReceiptJob_SuccessAndMatchedDefinition(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gormDB, mock := setupReceiptTest(t)

	handler := &ReceiptHandler{DB: gormDB}

	jobID := uuid.New()
	homeID := uuid.New()
	userID := uuid.New()
	defID := uuid.New()
	i18n.InvalidateUserLanguageCache(userID)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_jobs" WHERE "receipt_jobs"."id" = $1 ORDER BY "receipt_jobs"."id" LIMIT $2`)).
		WithArgs(jobID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "user_id", "status"}).
			AddRow(jobID, homeID, userID, "completed"))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_job_items" WHERE "receipt_job_items"."receipt_job_id" = $1`)).
		WithArgs(jobID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "receipt_job_id", "raw_name", "quantity", "price", "matched_item_definition_id", "confidence"}).
			AddRow(uuid.New(), jobID, "Organic Milk", 2.0, 4.99, &defID, 0.95))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
		WithArgs(defID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(defID, "Organic Milk"))

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
	assert.NotNil(t, resp.Items[0].MatchedItemDefinition)
}

func TestGetReceiptJob_Errors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gormDB, mock := setupReceiptTest(t)
	handler := &ReceiptHandler{DB: gormDB}
	jobID := uuid.New()
	homeID := uuid.New()
	userID := uuid.New()

	t.Run("invalid job ID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/receipts/jobs/invalid", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: "invalid"}}

		handler.GetReceiptJob(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("job not found", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_jobs"`)).
			WillReturnError(gorm.ErrRecordNotFound)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/receipts/jobs/"+jobID.String(), nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: jobID.String()}}

		handler.GetReceiptJob(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("access denied to home", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_jobs"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "user_id"}).AddRow(jobID, homeID, userID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_job_items"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id", "receipt_job_id"}))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes"`)).
			WillReturnError(gorm.ErrRecordNotFound)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/receipts/jobs/"+jobID.String(), nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: jobID.String()}}
		c.Set("userID", userID)

		handler.GetReceiptJob(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestConfirmReceiptJob_ErrorsAndTransactionFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gormDB, mock := setupReceiptTest(t)
	handler := &ReceiptHandler{DB: gormDB}
	jobID := uuid.New()
	homeID := uuid.New()
	userID := uuid.New()
	defID := uuid.New()

	t.Run("invalid job ID", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/receipts/jobs/invalid/confirm", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: "invalid"}}

		handler.ConfirmReceiptJob(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("job not found", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_jobs"`)).
			WillReturnError(gorm.ErrRecordNotFound)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/receipts/jobs/"+jobID.String()+"/confirm", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: jobID.String()}}

		handler.ConfirmReceiptJob(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("write access denied", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_jobs"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(jobID, homeID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes"`)).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "viewer"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/receipts/jobs/"+jobID.String()+"/confirm", nil)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: jobID.String()}}
		c.Set("userID", userID)

		handler.ConfirmReceiptJob(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid request payload", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_jobs"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(jobID, homeID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes"`)).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/receipts/jobs/"+jobID.String()+"/confirm", bytes.NewBufferString(`{"items":[]}`))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: jobID.String()}}
		c.Set("userID", userID)

		handler.ConfirmReceiptJob(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("item quantity <= 0 error", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_jobs"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(jobID, homeID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes"`)).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectRollback()

		reqPayload := ConfirmReceiptJobRequest{
			Items: []ConfirmReceiptItem{{RawName: "Milk", Quantity: 0}},
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
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("definition not found error", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_jobs"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(jobID, homeID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes"`)).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions"`)).
			WillReturnError(gorm.ErrRecordNotFound)
		mock.ExpectRollback()

		reqPayload := ConfirmReceiptJobRequest{
			Items: []ConfirmReceiptItem{{RawName: "Milk", Quantity: 1, ItemDefinitionID: &defID}},
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
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("failed to create inventory item in transaction", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_jobs"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(jobID, homeID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes"`)).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions"`)).
			WithArgs(defID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(defID, homeID, "Milk"))

		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "inventory_items"`)).
			WillReturnError(errors.New("db insert inv item error"))
		mock.ExpectRollback()

		reqPayload := ConfirmReceiptJobRequest{
			Items: []ConfirmReceiptItem{{RawName: "Milk", Quantity: 1, ItemDefinitionID: &defID}},
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
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("failed to create new item definition in transaction", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_jobs"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(jobID, homeID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes"`)).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "item_definitions"`)).
			WillReturnError(errors.New("db insert def error"))
		mock.ExpectRollback()

		reqPayload := ConfirmReceiptJobRequest{
			Items: []ConfirmReceiptItem{{RawName: "Butter", Quantity: 1, CreateItemDefinition: true}},
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
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("failed to create transaction log in transaction", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "receipt_jobs"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(jobID, homeID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes"`)).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions"`)).
			WithArgs(defID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(defID, homeID, "Milk"))

		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "inventory_items"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "inventory_transactions"`)).
			WillReturnError(errors.New("db insert tx error"))
		mock.ExpectRollback()

		reqPayload := ConfirmReceiptJobRequest{
			Items: []ConfirmReceiptItem{{RawName: "Milk", Quantity: 1, ItemDefinitionID: &defID}},
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
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestGetParserDefault(t *testing.T) {
	handler := &ReceiptHandler{}
	parser := handler.getParser()
	assert.NotNil(t, parser)
	_, isStd := parser.(*utils.StandardReceiptOCRParser)
	assert.True(t, isStd)
}
