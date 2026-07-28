### Competitor Analysis
* **Grocy**: Excellent self-hosted solution for groceries and chores. Lacks modern predictive insights and automated external recipe matching out-of-the-box.
* **KitchenPal**: Mobile-first, good at suggesting recipes based on pantry items, but lacks comprehensive home maintenance tracking.
* **Homebox**: Great modern UI for general inventory, but less focused on consumable food items and lacks smart shopping lists or recipe integrations.
* **PantryCheck**: Good expiry tracking but lacks smart calculations for maintenance or advanced budgeting.

### Feature Proposal: Smart Recipe Recommendations based on Expiring Inventory

**Business Need:**
Users often forget what they have in their inventory and let items expire, leading to food waste. By providing smart recipe recommendations based on items that are expiring soon (within 3-7 days), we can encourage consumption before expiration, saving users money and reducing waste.

**Acceptance Criteria:**
- [ ] System automatically identifies inventory items expiring within a configurable window (e.g., 7 days).
- [ ] Backend provides an endpoint `GET /api/v1/inventory/insights/recipes` that returns recipe suggestions utilizing these expiring items.
- [ ] The engine prioritizes recipes that use the highest quantity of expiring items.
- [ ] Frontend apps (Web/Mobile) display a "Suggested Recipes to reduce waste" widget on the dashboard.
- [ ] Users can click a recipe to see instructions and which inventory items it will consume.
- [ ] Users can mark a recipe as "Cooked", which triggers an automatic deduction of the required item quantities from their inventory (using FEFO).
