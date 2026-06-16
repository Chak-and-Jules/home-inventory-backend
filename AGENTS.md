# Agent Instructions

When working on this repository, please adhere to the following guidelines and instructions.

## Pre-Commit Checks

Before committing any code or marking a task as complete, you must run the following checks to ensure code quality and correctness. All of these commands should pass without errors:

1.  **Formatting**: Run `gofmt -s -w .` to format the Go code.
2.  **Static Analysis**: Run `go vet ./...` to catch common Go errors.
3.  **Compilation Checks**: Run `go build ./...` to ensure the code compiles successfully.
4.  **Testing**: Run `go test ./...` to ensure all tests pass.

Ensure that any changes you make do not break existing tests and that you write tests for any new functionality.

## Globalization Checks

- Check if there are any newly added strings exist in the changeset. If there is, make sure it is added in the `i18n.go` file. It also should be used through `TranslateDB` function.
- Check if an existing string is modified in this changeset. If it exists in the `i18n.go` file, make sure the key and translation values are also updated.

## API Endpoint Checks

- Whenever there is any change in the codebase that changes the api contract, the `openapi.json` file should also be updated and included in the commit.
- When the `openapi.json` file is updated, create an issue in the repository `https://github.com/Chak-and-Jules/home-inventory-web/issues` describing the change; and add `jules` label to it.

## Git Workflow

- Every code change must be pushed to the remote `https://github.com/Chak-and-Jules/home-inventory-backend` repository.
- Changes should be pushed to a new branch created using `main` as the base branch.
- Once pushed, a new Pull Request (PR) must be created to merge the new branch into the `main` branch.
