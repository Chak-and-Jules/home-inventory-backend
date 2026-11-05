package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

var sqliteUnitsOnce sync.Once

type unitFixture struct {
	db         *gorm.DB
	router     *gin.Engine
	home, user uuid.UUID
	units      map[string]uuid.UUID
	def        models.ItemDefinition
}

func newUnitFixture(t *testing.T) *unitFixture {
	t.Helper()
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	db := openUnitTestDB(t)
	var err error
	require.NoError(t, db.AutoMigrate(&models.Language{}, &models.Profile{}, &models.Home{}, &models.UserHome{}, &models.SizeUnit{}, &models.Category{}, &models.ItemDefinition{}, &models.InventoryItem{}, &models.InventoryTransaction{}, &models.ShoppingListItem{}, &models.MaintenanceTask{}, &models.TaskItemDependency{}, &models.ReceiptJob{}, &models.ReceiptJobItem{}, &models.Recipe{}, &models.RecipeIngredient{}, &models.InventoryPrediction{}))
	db, err = models.InitializeUnits(db)
	require.NoError(t, err)
	f := &unitFixture{db: db, home: uuid.New(), user: uuid.New(), units: map[string]uuid.UUID{}}
	var units []models.SizeUnit
	require.NoError(t, db.Find(&units).Error)
	for _, u := range units {
		f.units[u.Name] = u.ID
	}
	require.NoError(t, db.Create(&models.Home{ID: f.home, Name: "Home"}).Error)
	require.NoError(t, db.Create(&models.Profile{ID: f.user}).Error)
	require.NoError(t, db.Create(&models.UserHome{HomeID: f.home, UserID: f.user, Role: models.RoleOwner}).Error)
	kg := f.units["kg"]
	f.def = models.ItemDefinition{ID: uuid.New(), HomeID: f.home, Name: "Flour", SizeUnitID: &kg}
	require.NoError(t, db.Create(&f.def).Error)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", f.user) })
	f.router = r
	u := &SizeUnitHandler{DB: db}
	r.GET("/units", u.GetSizeUnits)
	h := &InventoryItemHandler{DB: db}
	r.GET("/inventory", h.GetInventoryItems)
	r.POST("/inventory", h.CreateInventoryItem)
	r.PUT("/inventory/:id", h.UpdateInventoryItem)
	r.PATCH("/inventory/:id", h.PatchInventoryItem)
	r.PATCH("/inventory/:id/quantity", h.UpdateInventoryItemQuantity)
	r.POST("/inventory/scan", h.ScanInventoryItem)
	r.PUT("/predictions/apply", h.ApplyPrediction)
	d := &ItemDefinitionHandler{DB: db}
	r.POST("/definitions", d.CreateItemDefinition)
	r.PUT("/definitions/:id", d.UpdateItemDefinition)
	s := &ShoppingListHandler{DB: db}
	r.POST("/shopping", s.CreateShoppingListItem)
	r.PUT("/shopping/:id", s.UpdateShoppingListItem)
	recipe := &RecipeHandler{DB: db}
	r.POST("/recipes", recipe.CreateRecipe)
	r.PUT("/recipes/:id", recipe.UpdateRecipe)
	r.POST("/recipes/:id/cook", recipe.CookRecipe)
	m := &MaintenanceTaskHandler{DB: db}
	r.POST("/maintenance", m.CreateMaintenanceTask)
	r.PUT("/maintenance/:id", m.UpdateMaintenanceTask)
	r.POST("/maintenance/:id/complete", m.CompleteMaintenanceTask)
	receipt := &ReceiptHandler{DB: db}
	r.POST("/receipts/:id/confirm", receipt.ConfirmReceiptJob)
	return f
}

// The same end-to-end cases run against isolated PostgreSQL schemas when a
// disposable database DSN is supplied, or in-memory SQLite in the default suite.
func openUnitTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	config := &gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)}
	if dsn := os.Getenv("UNIT_TEST_POSTGRES_DSN"); dsn != "" {
		admin, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), config)
		require.NoError(t, err)
		schema := "unit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
		db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn + " search_path=" + schema, PreferSimpleProtocol: true}), config)
		require.NoError(t, err)
		t.Cleanup(func() {
			conn, _ := db.DB()
			conn.Close()
			admin.Exec("DROP SCHEMA " + schema + " CASCADE")
			connection, _ := admin.DB()
			connection.Close()
		})
		return db
	}
	sqliteUnitsOnce.Do(func() {
		sql.Register("sqlite3_units", &sqlite3.SQLiteDriver{ConnectHook: func(c *sqlite3.SQLiteConn) error {
			if err := c.RegisterFunc("gen_random_uuid", uuid.NewString, false); err != nil {
				return err
			}
			return c.RegisterFunc("NOW", func() string { return time.Now().UTC().Format("2006-01-02 15:04:05") }, false)
		}})
	})
	db, err := gorm.Open(sqlite.New(sqlite.Config{DriverName: "sqlite3_units", DSN: fmt.Sprintf("file:%s?mode=memory&cache=shared&_foreign_keys=on", uuid.New())}), &gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { conn.Close() })
	// SQLite recognizes datetime for time scanning; production uses timestamptz.
	require.NoError(t, db.Callback().Raw().Before("gorm:raw").Register("test:sqlite_timestamp", func(tx *gorm.DB) {
		q := tx.Statement.SQL.String()
		tx.Statement.SQL.Reset()
		tx.Statement.SQL.WriteString(strings.ReplaceAll(q, "timestamp with time zone", "datetime"))
	}))
	return db
}

func (f *unitFixture) request(t *testing.T, method, path string, body any, want int) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Home-Id", f.home.String())
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	require.Equal(t, want, w.Code, w.Body.String())
	return w
}
func (f *unitFixture) stock(t *testing.T, value float64, unit string) models.InventoryItem {
	t.Helper()
	w := f.request(t, "POST", "/inventory", map[string]any{"item_definition_id": f.def.ID, "quantity": value, "quantity_unit_id": f.units[unit]}, 201)
	var item models.InventoryItem
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &item))
	return item
}

func TestUnitInventoryLifecycle(t *testing.T) {
	f := newUnitFixture(t)
	item := f.stock(t, 3, "kg")
	require.Equal(t, 3000.0, item.Quantity)
	require.Equal(t, f.units["g"], *item.BaseSizeUnitID)
	path := "/inventory/" + item.ID.String()
	expiry := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	f.request(t, "PATCH", path, map[string]any{"expiry_date": expiry}, 200)
	var expiryCheck models.InventoryItem
	require.NoError(t, f.db.First(&expiryCheck, item.ID).Error)
	f.request(t, "PATCH", path, map[string]any{"display_size_unit_id": f.units["g"]}, 200)
	var saved models.InventoryItem
	require.NoError(t, f.db.First(&saved, item.ID).Error)
	require.Equal(t, 3000.0, saved.Quantity)
	require.Equal(t, f.units["g"], *saved.DisplaySizeUnitID)
	require.True(t, saved.ExpirationDate.Equal(expiry))
	var count int64
	require.NoError(t, f.db.Model(&models.InventoryTransaction{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	f.request(t, "PATCH", path, map[string]any{"quantity": 800, "quantity_unit_id": f.units["g"]}, 200)
	f.request(t, "PATCH", path, map[string]any{"quantity": 0.8, "quantity_unit_id": f.units["kg"]}, 200)
	var txs []models.InventoryTransaction
	require.NoError(t, f.db.Order("created_at").Find(&txs).Error)
	require.Len(t, txs, 2)
	require.Equal(t, -2200.0, txs[1].QuantityChange)
	f.request(t, "PATCH", path, map[string]any{"expiry_date": expiry}, 200)
	require.NoError(t, f.db.First(&saved, item.ID).Error)
	require.Equal(t, f.units["g"], *saved.DisplaySizeUnitID)
	f.request(t, "PATCH", path, map[string]any{"display_size_unit_id": nil}, 200)
	var cleared models.InventoryItem
	require.NoError(t, f.db.Preload("ItemDefinition").First(&cleared, item.ID).Error)
	require.Nil(t, cleared.DisplaySizeUnitID)
	require.Equal(t, f.units["kg"], *cleared.EffectiveDisplaySizeUnitID)
	f.request(t, "PATCH", path, map[string]any{"display_size_unit_id": f.units["L"]}, 400)
	f.request(t, "PATCH", path, map[string]any{"quantity": 1}, 400)
	f.request(t, "PATCH", path, map[string]any{"quantity": 1, "quantity_unit_id": f.units["L"]}, 400)
	f.request(t, "PATCH", path, map[string]any{"quantity": 0, "quantity_unit_id": f.units["g"]}, 200)
}

func TestUnitCatalogAndDefaultChanges(t *testing.T) {
	f := newUnitFixture(t)
	_, err := models.InitializeUnits(f.db)
	require.NoError(t, err)
	var count int64
	require.NoError(t, f.db.Model(&models.SizeUnit{}).Count(&count).Error)
	require.EqualValues(t, 5, count)
	require.Error(t, f.db.Create(&models.SizeUnit{ID: uuid.New(), Name: "duplicate", Family: "mass", IsBase: true, FactorToBase: 1}).Error)
	require.Error(t, f.db.Create(&models.SizeUnit{ID: uuid.New(), Name: "bad", Family: "other", FactorToBase: 0}).Error)
	require.Error(t, f.db.Create(&models.SizeUnit{ID: uuid.New(), Name: "bad-base", Family: "other", IsBase: true, FactorToBase: 2}).Error)
	missing := models.SizeUnit{ID: uuid.New(), Name: "missing-base", Family: "new-family", FactorToBase: 10}
	require.NoError(t, f.db.Create(&missing).Error)
	_, err = models.LoadUnitCatalog(f.db)
	require.Error(t, err)
	require.NoError(t, f.db.Session(&gorm.Session{SkipHooks: true}).Delete(&missing).Error)
	item := f.stock(t, 0.8, "kg")
	override := f.stock(t, 0.5, "kg")
	f.request(t, "PATCH", "/inventory/"+override.ID.String(), map[string]any{"display_size_unit_id": f.units["kg"]}, 200)
	threshold, target := 1000.0, 5000.0
	require.NoError(t, f.db.Model(&f.def).Updates(map[string]any{"low_stock_threshold": threshold, "target_quantity": target}).Error)
	f.request(t, "PUT", "/definitions/"+f.def.ID.String(), map[string]any{"name": "Flour", "size_unit_id": f.units["g"]}, 200)
	var def models.ItemDefinition
	require.NoError(t, f.db.First(&def, f.def.ID).Error)
	require.Equal(t, threshold, *def.LowStockThreshold)
	require.Equal(t, target, *def.TargetQuantity)
	var inherited, explicit models.InventoryItem
	require.NoError(t, f.db.Preload("ItemDefinition").First(&inherited, item.ID).Error)
	require.NoError(t, f.db.Preload("ItemDefinition").First(&explicit, override.ID).Error)
	require.Equal(t, f.units["g"], *inherited.EffectiveDisplaySizeUnitID)
	require.Equal(t, f.units["kg"], *explicit.EffectiveDisplaySizeUnitID)
	require.Equal(t, 1300.0, inherited.Quantity+explicit.Quantity)
	f.request(t, "PUT", "/definitions/"+f.def.ID.String(), map[string]any{"name": "Flour", "size_unit_id": f.units["L"]}, 400)
	f.request(t, "PATCH", "/inventory/"+item.ID.String(), map[string]any{"display_size_unit_id": f.units["g"]}, 200)
	f.request(t, "GET", "/inventory", nil, 200)
}

func TestUnitStockWritingFlows(t *testing.T) {
	t.Run("barcode and prediction deductions", func(t *testing.T) {
		f := newUnitFixture(t)
		item := f.stock(t, 3, "kg")
		require.NoError(t, f.db.Model(&f.def).Update("barcode", "flour").Error)
		f.request(t, "POST", "/inventory/scan", map[string]any{"barcode": "flour", "change": -0.25, "quantity_unit_id": f.units["kg"]}, 200)
		p := models.InventoryPrediction{ID: uuid.New(), InventoryItemID: item.ID, PredictedConsumedAmount: 500, Status: models.PredictionStatusPredicted}
		require.NoError(t, f.db.Create(&p).Error)
		f.request(t, "PUT", "/predictions/apply", map[string]any{"prediction_id": p.ID, "applied_amount": 0.5, "quantity_unit_id": f.units["kg"]}, 200)
		f.request(t, "PUT", "/predictions/apply", map[string]any{"prediction_id": p.ID, "applied_amount": 0.5, "quantity_unit_id": f.units["kg"]}, 404)
		var saved models.InventoryItem
		require.NoError(t, f.db.First(&saved, item.ID).Error)
		require.Equal(t, 2250.0, saved.Quantity)
		f.request(t, "POST", "/inventory/scan", map[string]any{"barcode": "flour", "change": -10, "quantity_unit_id": f.units["kg"]}, 200)
		var txs []models.InventoryTransaction
		require.NoError(t, f.db.Order("created_at").Find(&txs).Error)
		require.Len(t, txs, 4)
		require.Equal(t, -250.0, txs[1].QuantityChange)
		require.Equal(t, -500.0, txs[2].QuantityChange)
		require.Equal(t, -2250.0, txs[3].QuantityChange)
	})
	t.Run("recipe creation update and cooking", func(t *testing.T) {
		f := newUnitFixture(t)
		item := f.stock(t, 1, "kg")
		body := map[string]any{"name": "Bread", "servings": 2, "ingredients": []any{map[string]any{"item_definition_id": f.def.ID, "quantity_required": 0.25, "quantity_unit_id": f.units["kg"]}}}
		w := f.request(t, "POST", "/recipes", body, 201)
		var recipe models.Recipe
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recipe))
		var ing models.RecipeIngredient
		require.NoError(t, f.db.Where("recipe_id = ?", recipe.ID).First(&ing).Error)
		require.Equal(t, 250.0, ing.QuantityRequired)
		body["ingredients"] = []any{map[string]any{"item_definition_id": f.def.ID, "quantity_required": 0.125, "quantity_unit_id": f.units["kg"]}}
		f.request(t, "PUT", "/recipes/"+recipe.ID.String(), body, 200)
		f.request(t, "POST", "/recipes/"+recipe.ID.String()+"/cook", map[string]any{}, 200)
		var saved models.InventoryItem
		require.NoError(t, f.db.First(&saved, item.ID).Error)
		require.Equal(t, 875.0, saved.Quantity)
	})
	t.Run("maintenance dependency and deduction", func(t *testing.T) {
		f := newUnitFixture(t)
		item := f.stock(t, 1, "kg")
		body := map[string]any{"description": "Maintain", "scheduled_date": time.Now().UTC().Add(time.Hour), "frequency": "Once", "dependencies": []any{map[string]any{"item_definition_id": f.def.ID, "quantity_required": 0.1, "quantity_unit_id": f.units["kg"]}}}
		w := f.request(t, "POST", "/maintenance", body, 201)
		var task models.MaintenanceTask
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &task))
		require.Len(t, task.Dependencies, 1)
		require.Equal(t, 100.0, task.Dependencies[0].QuantityRequired)
		body["dependencies"] = []any{map[string]any{"item_definition_id": f.def.ID, "quantity_required": 0.05, "quantity_unit_id": f.units["kg"]}}
		f.request(t, "PUT", "/maintenance/"+task.ID.String(), body, 200)
		f.request(t, "POST", "/maintenance/"+task.ID.String()+"/complete", map[string]any{}, 200)
		var saved models.InventoryItem
		require.NoError(t, f.db.First(&saved, item.ID).Error)
		require.Equal(t, 950.0, saved.Quantity)
	})
	t.Run("shopping and thresholds", func(t *testing.T) {
		f := newUnitFixture(t)
		w := f.request(t, "POST", "/shopping", map[string]any{"item_definition_id": f.def.ID, "quantity": 1.5, "quantity_unit_id": f.units["kg"]}, 201)
		var shopping models.ShoppingListItem
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &shopping))
		require.Equal(t, 1500.0, shopping.Quantity)
		f.request(t, "PUT", "/shopping/"+shopping.ID.String(), map[string]any{"quantity": 1, "quantity_unit_id": f.units["kg"]}, 200)
		w = f.request(t, "POST", "/shopping", map[string]any{"name": "Sponges", "quantity": 3}, 201)
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &shopping))
		require.Equal(t, 3.0, shopping.Quantity)
		w = f.request(t, "POST", "/definitions", map[string]any{"name": "Milk", "size_unit_id": f.units["L"], "low_stock_threshold": 1, "target_quantity": 3, "quantity_unit_id": f.units["L"]}, 201)
		var def models.ItemDefinition
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &def))
		require.Equal(t, 1000.0, *def.LowStockThreshold)
		require.Equal(t, 3000.0, *def.TargetQuantity)
		var generated models.ShoppingListItem
		require.NoError(t, f.db.Where("item_definition_id = ?", def.ID).First(&generated).Error)
		require.Equal(t, 3000.0, generated.Quantity)
	})
	t.Run("receipt normalization and rollback", func(t *testing.T) {
		f := newUnitFixture(t)
		job := models.ReceiptJob{ID: uuid.New(), HomeID: f.home, UserID: f.user, Status: "completed"}
		require.NoError(t, f.db.Create(&job).Error)
		f.request(t, "POST", "/receipts/"+job.ID.String()+"/confirm", map[string]any{"items": []any{map[string]any{"raw_name": "Flour", "item_definition_id": f.def.ID, "quantity": 1.25, "quantity_unit_id": f.units["kg"]}, map[string]any{"raw_name": "Milk", "quantity": 750, "quantity_unit_id": f.units["mL"], "size_unit_id": f.units["L"]}}}, 200)
		var stock []models.InventoryItem
		require.NoError(t, f.db.Order("quantity DESC").Find(&stock).Error)
		require.Len(t, stock, 2)
		require.Equal(t, 1250.0, stock[0].Quantity)
		require.Equal(t, 750.0, stock[1].Quantity)
		f.request(t, "POST", "/receipts/"+job.ID.String()+"/confirm", map[string]any{"items": []any{map[string]any{"raw_name": "Bad", "quantity": 1, "quantity_unit_id": f.units["kg"], "size_unit_id": f.units["L"]}}}, 400)
		var count int64
		require.NoError(t, f.db.Model(&models.ItemDefinition{}).Where("name = ?", "Bad").Count(&count).Error)
		require.Zero(t, count)
	})
}

func TestUnitValidationAuthorizationAndAtomicity(t *testing.T) {
	f := newUnitFixture(t)
	item := f.stock(t, 0.8, "kg")
	path := "/inventory/" + item.ID.String()
	for _, body := range []map[string]any{
		{"quantity": 1, "quantity_unit_id": uuid.New()},
		{"quantity": 1_000_001, "quantity_unit_id": f.units["kg"]},
		{"quantity": 0.0000001, "quantity_unit_id": f.units["g"]},
		{"quantity": -1, "quantity_unit_id": f.units["g"]},
	} {
		f.request(t, "PATCH", path, body, 400)
	}
	if f.db.Dialector.Name() == "postgres" {
		require.NoError(t, f.db.Exec("CREATE FUNCTION fail_unit_tx() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced transaction failure'; END $$").Error)
		require.NoError(t, f.db.Exec("CREATE TRIGGER fail_unit_tx BEFORE INSERT ON inventory_transactions FOR EACH ROW EXECUTE FUNCTION fail_unit_tx()").Error)
	} else {
		require.NoError(t, f.db.Exec("CREATE TRIGGER fail_unit_tx BEFORE INSERT ON inventory_transactions BEGIN SELECT RAISE(ABORT,'forced transaction failure'); END").Error)
	}
	f.request(t, "PATCH", path, map[string]any{"quantity": 500, "quantity_unit_id": f.units["g"]}, 500)
	var saved models.InventoryItem
	require.NoError(t, f.db.First(&saved, item.ID).Error)
	require.Equal(t, 800.0, saved.Quantity)
	require.NoError(t, f.db.Model(&models.UserHome{}).Where("user_id = ? AND home_id = ?", f.user, f.home).Update("role", models.RoleViewer).Error)
	f.request(t, "PATCH", path, map[string]any{"display_size_unit_id": f.units["g"]}, 403)
	foreign := uuid.New()
	f.request(t, "POST", "/inventory", map[string]any{"item_definition_id": foreign, "quantity": 1, "quantity_unit_id": f.units["g"]}, 403)
}

func TestUnitExactJSONDecimals(t *testing.T) {
	f := newUnitFixture(t)
	item := f.stock(t, 0.8, "kg")
	tooPrecise := json.RawMessage("0.10000000000000000001")
	f.request(t, "POST", "/inventory", map[string]any{"item_definition_id": f.def.ID, "quantity": tooPrecise, "quantity_unit_id": f.units["g"]}, 400)
	f.request(t, "PATCH", "/inventory/"+item.ID.String(), map[string]any{"quantity": tooPrecise, "quantity_unit_id": f.units["g"]}, 400)
	f.request(t, "POST", "/shopping", map[string]any{"name": "Unlinked count", "quantity": tooPrecise}, 400)
	f.request(t, "POST", "/shopping", map[string]any{"item_definition_id": f.def.ID, "quantity": tooPrecise, "quantity_unit_id": f.units["g"]}, 400)
	f.request(t, "POST", "/definitions", map[string]any{"name": "Precise", "size_unit_id": f.units["g"], "low_stock_threshold": tooPrecise, "quantity_unit_id": f.units["g"]}, 400)
	f.request(t, "PATCH", "/inventory/"+item.ID.String(), map[string]any{"quantity": nil, "display_size_unit_id": f.units["g"]}, 400)
	f.request(t, "PATCH", "/inventory/"+item.ID.String(), map[string]any{"expiry_date": "bad date"}, 400)
	tiny := f.stock(t, 0.000000001, "kg")
	require.Equal(t, 0.000001, tiny.Quantity)
	var units []models.SizeUnit
	w := f.request(t, "GET", "/units", nil, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &units))
	for _, u := range units {
		require.NotNil(t, u.BaseSizeUnitID)
	}
}
