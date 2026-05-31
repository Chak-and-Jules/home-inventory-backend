💡 **What:**
Replaced `.Count(&homeCount)` with `.Select("1").Limit(1).Find(&exists)` when checking if a user has any existing homes in the profile sync handler.

🎯 **Why:**
Using `Count(*)` on a table can be expensive in Postgres as it requires scanning all rows that match the condition. Since we only need to know if at least *one* home exists (a boolean check `homeCount == 0`), doing a `Select("1")` with `Limit(1)` is an O(1) existence check, which is significantly faster and more scalable as the number of user homes grows.

📊 **Measured Improvement:**
A benchmark was created comparing `Count` vs `Find` with `Limit(1)`. Although the mocked benchmark shows similar timings (~185,784 ns/op vs ~189,624 ns/op) because it only measures go-sqlmock overhead, in a real PostgreSQL database, `.Count(&homeCount)` performs a full index scan (or table scan) to count matching rows, taking O(N) time. The updated query (`SELECT 1 ... LIMIT 1`) safely returns after finding the first match in O(1) time.
