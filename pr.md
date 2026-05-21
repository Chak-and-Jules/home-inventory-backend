⚡ Implement in-memory caching for item definition retrieval

💡 **What:**
Added a simple thread-safe in-memory cache to `ItemDefinitionHandler`. The cache stores the `[]models.ItemDefinition` retrieved from the database and uses a `sync.RWMutex` to manage concurrent reads and cache invalidation. The cache is invalidated (`cacheValid = false`) on any Create, Update, or Delete operation.

🎯 **Why:**
The `GetItemDefinitions` endpoint was previously fetching all item definitions from the database on every request. Item definitions act as blueprint metadata, which are read frequently but updated rarely. Caching them in memory drastically reduces database load and speeds up API response times.

📊 **Measured Improvement:**
A benchmark was created (`BenchmarkGetItemDefinitions`) measuring performance before and after caching.

**Baseline (Before Caching):**
* ~1,333,809 ns/op
* ~437 allocs/op (50,507 B/op)

**After Caching:**
* ~22,508 ns/op
* ~88 allocs/op (7,144 B/op)

**Improvement:**
This resulted in an ~59x performance improvement (speed) and significantly fewer allocations for cache hits.
