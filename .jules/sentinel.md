## 2024-05-24 - [Information Exposure via Error Messages]
**Vulnerability:** Internal errors and database error messages were appended to HTTP JSON error responses by concatenating `err.Error()`.
**Learning:** Returning `err.Error()` in HTTP handlers is dangerous since the underlying driver or ORM can leak structural data (like database query details or schemas).
**Prevention:** Avoid interpolating `err.Error()` in production HTTP JSON responses. Implement generic error messages for end users and use secure logging tools for the actual error content.
