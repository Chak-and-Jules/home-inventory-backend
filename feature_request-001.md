# Feature Request: Automated Shopping List Generation based on Low Stock Thresholds

## Market Investigation & Competitor Analysis
We conducted an investigation to evaluate market options for home inventory management, specifically looking at how competitors handle inventory tracking and automatic shopping list creation.

**1. KitchenPal (iCuisto)**
- **Features:** Comprehensive kitchen/pantry management, barcode scanning, AI photo scanning, proactive expiration alerts, and recipe suggestions based on current inventory. It automatically creates shopping lists.
- **Usability/Usefulness:** Highly useful for kitchen-focused households. The proactive AI planning and automatic grocery list based on inventory consumption significantly reduce cognitive load.

**2. Sortly**
- **Features:** Focused on general home organizing, tracking valuables and collections. It features QR/barcode generation, high-resolution photo uploads, custom folders, and fields.
- **Usability/Usefulness:** Extremely useful for tracking valuable items, insurance reporting, and moving. However, it lacks grocery-specific automated features like dynamic shopping lists.

**3. Spullio (and HomeManage)**
- **Features:** General home inventory app alternatives with AI photo recognition, family household sharing, loan center, and barcode scanning. HomeManage is a desktop app focused on insurance and barcodes.
- **Usability/Usefulness:** Excellent for detailed tracking and asset management, but missing seamless automated replenishment workflows for everyday consumables.

**Comparison to Our Product:**
Our current system supports `ItemDefinition` (blueprint with `LowStockThreshold`) and `InventoryItem` (instance with `Quantity`). However, we currently lack a proactive mechanism to help users seamlessly replenish items. Competitors like KitchenPal thrive because they translate low inventory into actionable shopping lists automatically.

## Selected Feature
**Automated Shopping List Generation based on Low Stock Thresholds**
We will implement a feature that automatically generates and manages a shopping list for each home based on the `LowStockThreshold` set on `ItemDefinition` and the current `Quantity` of the associated `InventoryItem`.

---

## Business Needs
1. **Reduce User Friction:** Users shouldn't have to manually check their inventory and then manually write down what they need to buy. The system should tell them.
2. **Prevent Stockouts:** Ensure essential household items are restocked before they completely run out by leveraging the user-defined `LowStockThreshold`.
3. **Enhance Multi-tenant Value:** Since inventory is shared within a "Home", the auto-generated shopping list should be available to all users with access to that home, acting as a centralized and synchronized household grocery list.

---

## Acceptance Criteria

### 1. Backend / Core Logic
- **AC 1.1:** A new `ShoppingList` or `ShoppingListItem` entity is introduced, linked to a specific `Home` and optionally an `ItemDefinition`.
- **AC 1.2:** When an `InventoryItem`'s `Quantity` drops below its `ItemDefinition`'s `LowStockThreshold` (either via transaction or direct update), the system automatically creates or updates an entry in the active shopping list for that home.
- **AC 1.3:** The suggested quantity to buy should be calculated (e.g., Target Quantity - Current Quantity) or default to a standard unit.
- **AC 1.4:** API endpoints must be created to fetch the current shopping list (`GET /homes/:home_id/shopping-list`), update items (e.g., mark as bought/crossed off), and manually add/remove items.
- **AC 1.5:** When a shopping list item is marked as "bought" or its quantity is replenished in the inventory, the item should be automatically resolved or removed from the active shopping list.

### 2. Web & Mobile App (Frontend)
- **AC 2.1:** A dedicated "Shopping List" view is available from the main navigation.
- **AC 2.2:** The Shopping List view clearly distinguishes between "Automatically Added" (due to low stock) and "Manually Added" items.
- **AC 2.3:** Users can manually add items to the shopping list.
- **AC 2.4:** Users can easily check off items while shopping. Checking off an item should optionally prompt the user to automatically update the inventory with the purchased quantity.
- **AC 2.5:** Notifications (push/in-app) are triggered when high-priority items hit their low stock threshold and are added to the list.

### 3. General & Cross-Platform
- **AC 3.1:** Full localization support (i18n) for all new UI text and automated system messages.
- **AC 3.2:** Changes adhere to home-scoped authorization (only Owner/Editor can modify the list, Viewers can only read).
