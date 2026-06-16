# Agent Instructions

When working on this repository, please adhere to the following guidelines and instructions.

## Pre-Commit Checks

Before committing any code or marking a task as complete, you must run the following checks to ensure code quality and correctness. All of these commands should pass without errors:

1.  **Formatting**: Run `gofmt -s -w .` to format the Go code.
2.  **Static Analysis**: Run `go vet ./...` to catch common Go errors.
3.  **Compilation Checks**: Run `go build ./...` to ensure the code compiles successfully.
4.  **Testing**: Run `go test ./...` to ensure all tests pass.

Ensure that any changes you make do not break existing tests and that you write tests for any new functionality.

## Additional Checks

- Whenever there is any change in the codebase that changes the api contract, the `openapi.json` file should also be updated and included in the commit.
- Whenever a new hardcoded string is added or an existing one is changed; if that string is being sent to the client, then it has to be reflected in the `i18n.go` file accordingly. And the usage should be through `TranslateDB` function.

## Git Workflow

- Every code change must be pushed to the remote `https://github.com/Chak-and-Jules/home-inventory-backend` repository.
- Changes should be pushed to a new branch created using `main` as the base branch.
- Once pushed, a new Pull Request (PR) must be created to merge the new branch into the `main` branch.
