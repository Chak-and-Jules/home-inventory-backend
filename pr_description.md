🔒 [Security] Restrict Category and Item Definition modification to home owners and editors

🎯 What:
- Added missing authorization checks for Category and Item Definition endpoints (Create, Update, Delete).
- Added `HomeID` to `Category` and `ItemDefinition` models so that they are scoped to a specific home instead of being globally shared.
- Updated `InventoryItem` and `Home` models to ensure relationships are well defined and consistent.
- Extracted common authorization logic (verifying user home access) into `internal/utils/auth_helpers.go` for shared usage across endpoints.
- Removed invalid global cache implementation for categories and item definitions.

⚠️ Risk:
- If left unfixed, any authenticated user could modify or delete categories and item definitions that belonged to other homes, potentially disrupting data for all users across the system. This is an Insecure Direct Object Reference (IDOR) and broken access control vulnerability.

🛡️ Solution:
- Added `HomeID` to explicitly tie Categories and Item Definitions to a home.
- Ensured `Create`, `Update`, and `Delete` operations explicitly require `home_id`.
- Used `VerifyHomeWriteAccess` to ensure only the `"owner"` and `"editor"` roles can modify Categories and Item Definitions.
- Used `VerifyHomeAccess` to ensure only permitted users can fetch (Read) the Categories and Item Definitions for a specific home.
- Fully tested all modified endpoints to correctly return `403 Forbidden` when users lack the requisite role.
