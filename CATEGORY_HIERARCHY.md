# Category hierarchy

Categories have at most two levels: a root such as Kitchen and children such as
Drinks. Children may have siblings, but may not have children. This restriction
applies to categories; the separate storage-location feature has its own rules.

## API behavior

`POST /api/v1/categories` and `PUT /api/v1/categories/{id}` accept `name` and
optional `parent_id`. A supplied parent must exist, belong to the same home, and
be a root (`parent_id IS NULL`). A category with children must remain a root.
Invalid moves are rejected; the backend never moves descendants implicitly.

PUT retains its replacement behavior: omitted or null `parent_id` makes the
category a root. To rename a child while retaining its parent, send its current
`parent_id`. Moving a leaf between roots, promoting a leaf to a root, and renaming
a root that has children are supported. Normal owner/editor permissions and
case-insensitive, space-ignoring uniqueness within each sibling group still apply.

Responses retain the existing `{ "error": "localized text" }` shape:

| Status | Cause |
| --- | --- |
| 400 | Invalid input, a child selected as parent, moving a branch under a parent, self-parenting, or a parent from another home |
| 403 | Missing home write permission |
| 404 | Category, parent, or home no longer exists |
| 409 | A normalized name already exists in the resulting sibling group |
| 500 | Database/transaction failure |

New hierarchy messages are translated with TranslateDB into English/Turkish.
Rejections roll back all changes. Validation, sibling-name checks and writes run
in one transaction after `SELECT id FROM homes ... FOR UPDATE`. Create/update/
delete use the same home lock. In PostgreSQL's normal READ COMMITTED isolation,
a waiting writer then validates the committed tree, including children created
by the previous writer. This prevents opposing moves or create-versus-move races.
Different homes use different locks. This is API enforcement, not a database
trigger; direct SQL/import writers must obey the same rule and locking protocol.

`GET /api/v1/categories` remains a flat array containing every category in the
selected home. Each `Parent` embeds only an immediate parent snapshot, whose own
`Parent` is null. Original `ParentID` values remain visible, including on malformed
legacy records. A parent outside the selected home (or missing from the result)
is not embedded. Cycles and deep legacy trees therefore produce bounded JSON
without rewriting data or exposing another home's categories.

## Read-only PostgreSQL audit

Run against the intended database with an account authorized to read the relevant
homes. These queries report relationships; they do not repair them. The examples
cover all homes; add an authorized `home_id` filter when auditing one home.

```sql
BEGIN TRANSACTION READ ONLY;
SET LOCAL statement_timeout = '30s';

-- Every edge whose parent is already a child: third/deeper levels and cycles.
SELECT c.home_id, c.id AS category_id, c.name,
       p.id AS parent_id, p.name AS parent_name, p.parent_id AS grandparent_id
FROM categories c
JOIN categories p ON p.id = c.parent_id
WHERE p.parent_id IS NOT NULL
ORDER BY c.home_id, c.id;

-- Other malformed edges, including self-parenting and cross-home links.
SELECT c.home_id, c.id AS category_id, c.name, c.parent_id,
       p.home_id AS parent_home_id
FROM categories c
LEFT JOIN categories p ON p.id = c.parent_id
WHERE c.parent_id IS NOT NULL
  AND (p.id IS NULL OR p.home_id <> c.home_id OR p.id = c.id)
ORDER BY c.home_id, c.id;

-- Follow parent links, retaining a path and stopping on the first repeated ID.
-- Each reported starting category reaches a cycle; paths may describe the same
-- cycle more than once. Cross-home edges are reported separately above.
WITH RECURSIVE ancestors AS (
    SELECT c.home_id, c.id AS start_id, c.id AS current_id, c.parent_id,
           ARRAY[c.id] AS path, false AS is_cycle
    FROM categories c
    UNION ALL
    SELECT a.home_id, a.start_id, p.id, p.parent_id,
           a.path || p.id, p.id = ANY(a.path)
    FROM ancestors a
    JOIN categories p ON p.id = a.parent_id AND p.home_id = a.home_id
    WHERE NOT a.is_cycle
)
SELECT home_id, start_id, path
FROM ancestors
WHERE is_cycle
ORDER BY home_id, start_id;

ROLLBACK;
```

Review a proposed repair separately. Preserve category IDs and existing
`item_definitions.category_id` assignments. Check normalized names in each
proposed sibling group before reparenting: flattening branches can collide with
existing names. Back up affected records and agree on the desired parents/names
before any write. No migration or automatic production-data cleanup accompanies
this fix. Existing invalid records may need a reviewed repair before normal
edits that retain an invalid parent will succeed.

## Verification

Database-backed handler tests cover invalid and valid operations, unchanged
relationships/item assignments on rejection, permissions, uniqueness, bounded
legacy listing, rollback and translations. The default suite uses SQLite with
foreign keys; concurrency requires disposable PostgreSQL:

```sh
UNIT_TEST_POSTGRES_DSN='host=127.0.0.1 port=55445 user=postgres password=test dbname=test sslmode=disable' go test -race ./internal/handlers -run '^TestCategoryHierarchy' -count=1
```

The fixture creates/drops isolated schemas and needs schema creation permission.
Use a disposable database, never production.
