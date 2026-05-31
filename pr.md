# 🧹 [code health improvement] extract duplicated getUserHome and verifyHomeWriteAccess into shared utility functions

**🎯 What:**
Extracted the `getUserHome`, `verifyHomeAccess`, and `verifyHomeWriteAccess` methods that were duplicated across `internal/handlers/item_definition.go` and `internal/handlers/inventory_item.go` into a centralized `internal/utils/auth_helpers.go` file. Updated handlers and test files to use these new utility functions.

**💡 Why:**
Removing duplication makes the codebase DRYer (Don't Repeat Yourself), improving maintainability. This shared utility now provides a single source of truth for checking home permissions, making future changes to access control logic easier and reducing the risk of inconsistent behavior.

**✅ Verification:**
- `go fmt ./...` and `go vet ./...` executed cleanly.
- `go build ./...` compiled without issues.
- `go test ./...` passed all tests successfully.
- Verified that all unit tests specifically covering these handler permissions continue to assert correctly with the updated utility functions.

**✨ Result:**
Reduced redundant code by extracting it into a common utility package without modifying underlying business logic, creating a cleaner and more maintainable `handlers` module.
