1. **Identify the optimization opportunity:** In `internal/handlers/shopping_list.go`, the `generatePredictiveSuggestions` function is retrieving entire rows using `Find(&modelSlice)` without specifying specific columns. This happens for `itemDefs`, `allItems`, `maintenanceTasks`, and `existingListItems`. This pulls unnecessary data into memory (like `ImageURL`, `Description`, `CreatedAt`, `UpdatedAt` etc) when only a few fields are needed for calculations, significantly inflating memory consumption and database transfer overhead. We can add `.Select` clauses to these queries to fetch only the required fields. For example, for `allItems` we only need `item_definition_id`, `quantity`, and `expiration_date`.
2. **Review similar handler:** `GetPredictiveRestockInsights` in `internal/handlers/inventory_item.go` performs similar logic and might also be fetching full models using `.Find(&allItems)`. Wait, let me check. Yes! `GetPredictiveRestockInsights` does this too. I can optimize both. Wait, `generatePredictiveSuggestions` does the exact same calculation! Wait, in `GetPredictiveRestockInsights` it uses `Preload("Category").Preload("SizeUnit")` for `itemDefs` because it returns them in the response. But `generatePredictiveSuggestions` in `shopping_list.go` does not return `itemDefs` in the response, it only uses them internally. Thus, we can optimize `generatePredictiveSuggestions` heavily with `.Select`.
3. **Plan the changes:**
  - In `internal/handlers/shopping_list.go:generatePredictiveSuggestions`:
    - `itemDefs`: change to `.Select("id, home_id, name, low_stock_threshold, target_quantity")`
    - `allItems`: change to `.Select("id, home_id, item_definition_id, quantity, expiration_date")` (id/home_id needed for GORM struct filling sometimes, but let's be safe and include them)
    - `maintenanceTasks`: for `maintenanceTasks`, it uses `Preload("Dependencies")` which requires the primary key. So `.Select("id, home_id, description, scheduled_date, frequency, custom_frequency, custom_frequency_metric, is_completed")` for `maintenanceTasks`.
    - `existingListItems`: `.Select("id, home_id, item_definition_id, is_predictive, is_dismissed")`
  - Update `internal/handlers/shopping_list_test.go` to match the new `Select` queries in the mock expectations by updating the regex or arguments. Note: GORM's `.Select` will change the generated query to `SELECT "field1", "field2" FROM "table"`.
4. **Alternative Optimization:** The background refresh fetches everything, but wait, `generatePredictiveSuggestions` is called on *every* `GetShoppingList` request. Running these 4 queries (even with `.Select`) on every fetch of the shopping list is quite expensive. However, adding just `Select` is safe and measurable (reduced allocation). Let's see if there is another optimization.
  Wait, `itemsByDef` groups all items by definition. The loop in `generatePredictiveSuggestions` does:
  ```go
		items := itemsByDef[def.ID]
		var currentStock float64
		for _, item := range items {
			if item.ExpirationDate == nil || item.ExpirationDate.After(now) {
				currentStock += item.Quantity
			}
		}
  ```
  Instead of fetching all inventory items into Go memory and doing a loop, we could do this in the database using a `GROUP BY` query:
  ```go
  type StockStat struct {
      ItemDefinitionID uuid.UUID
      CurrentStock float64
  }
  var stockStats []StockStat
  h.DB.Model(&models.InventoryItem{}).
      Select("item_definition_id, SUM(quantity) as current_stock").
      Where("home_id = ? AND (expiration_date IS NULL OR expiration_date > ?)", homeID, now).
      Group("item_definition_id").
      Find(&stockStats)
  ```
  This is a MUCH better performance optimization! It replaces fetching hundreds/thousands of inventory items into a Go slice and looping through them with a single aggregated DB query. This aligns perfectly with Bolt's principles ("offload large data aggregations to the database using GORM's Select().Group() clauses"). Wait, memory rule specifically says:
  "To optimize performance and reduce memory allocations/CPU overhead in Go space, offload large data aggregations (like SUM, MIN, MAX) to the database using GORM's Select().Group() clauses instead of fetching individual records into memory with Find() and iterating over them manually."
  This applies to both `generatePredictiveSuggestions` (in `shopping_list.go`) AND `GetPredictiveRestockInsights` (in `inventory_item.go`).
  I will optimize `generatePredictiveSuggestions` and `GetPredictiveRestockInsights` by replacing the `Find(&allItems)` and manual summation loop with a DB-level grouping!

5. **Let's refine the plan for the grouping query:**
  In `internal/handlers/shopping_list.go` `generatePredictiveSuggestions`:
  ```go
  // Old:
  var allItems []models.InventoryItem
  h.DB.Where("home_id = ?", homeID).Find(&allItems)
  itemsByDef := make(map[uuid.UUID][]models.InventoryItem)
  for _, item := range allItems {
      itemsByDef[item.ItemDefinitionID] = append(itemsByDef[item.ItemDefinitionID], item)
  }
  // ... loop later ...
		items := itemsByDef[def.ID]
		var currentStock float64
		for _, item := range items {
			if item.ExpirationDate == nil || item.ExpirationDate.After(now) {
				currentStock += item.Quantity
			}
		}
  ```
  ```go
  // New:
  type StockStat struct {
      ItemDefinitionID uuid.UUID
      CurrentStock     float64
  }
  var stockStats []StockStat
  h.DB.Model(&models.InventoryItem{}).
      Select("item_definition_id, SUM(quantity) as current_stock").
      Where("home_id = ? AND (expiration_date IS NULL OR expiration_date > ?)", homeID, now).
      Group("item_definition_id").
      Find(&stockStats)
  stockByDef := make(map[uuid.UUID]float64)
  for _, stat := range stockStats {
      stockByDef[stat.ItemDefinitionID] = stat.CurrentStock
  }
  // ... loop later ...
      currentStock := stockByDef[def.ID]
  ```
  Same for `internal/handlers/inventory_item.go` `GetPredictiveRestockInsights`.

6. **Let's review the tests:**
  We must update `shopping_list_test.go` and `predictive_restock_test.go` mock expectations.
  `mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1`))` ->
  `mock.ExpectQuery(regexp.QuoteMeta(`SELECT item_definition_id, SUM(quantity) as current_stock FROM "inventory_items" WHERE home_id = $1 AND (expiration_date IS NULL OR expiration_date > $2) GROUP BY "item_definition_id"`))` (or similar regex based on GORM output).

  Let's verify GORM output for that query:
  `SELECT item_definition_id, SUM(quantity) as current_stock FROM "inventory_items" WHERE home_id = $1 AND (expiration_date IS NULL OR expiration_date > $2) GROUP BY "item_definition_id"`
  GORM might omit quotes, so using broad regex `(?i)SELECT item_definition_id, SUM\(quantity\) as current_stock FROM "inventory_items" WHERE home_id = \$1 AND \(expiration_date IS NULL OR expiration_date > \$2\)` is safer. Wait, memory says: "When mocking GORM queries with go-sqlmock, use case-insensitive regex (?i) and broad matchers (e.g., .*id.* = $1.*)".

  Wait, there is also `.jules/bolt.md` for journal. I will add an entry.

7. **Execute:**
  - Update `internal/handlers/shopping_list.go`
  - Update `internal/handlers/shopping_list_test.go`
  - Update `internal/handlers/inventory_item.go`
  - Update `internal/handlers/predictive_restock_test.go`
  - Run tests. Ensure they pass.
  - Create `bolt.md` entry.
