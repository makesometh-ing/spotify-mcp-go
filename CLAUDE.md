# Development Rules

## TDD

Write tests first, then implementation. No exceptions.

1. Write a failing test for the behaviour you're about to implement
2. Run it, confirm it fails
3. Write the minimum code to make it pass
4. Refactor if needed
5. Repeat

Integration tests are preferred over unit tests. Use unit tests only for pure logic with no external dependencies.

## Commit Conventions

Follow https://www.conventionalcommits.org/en/v1.0.0/#summary with a mandatory scope and Linear issue suffix.

Format: `<type>(<scope>): <description> [<LINEAR-ID>]`

Example: `feat(auth): implement /register endpoint [SPO-12]`

Linear ID is mandatory. Every commit ties back to a Linear issue. If no issue exists, create one first.

## Linear Integration

This project uses the Linear CLI for issue management. The team is `SPO` in the `make-something` workspace.

- Never start work without a Linear issue
- Use `linear issue start <ID>` to create a branch and move to In Progress
- Commit messages reference the issue ID
- PRs are created via `linear issue pr`

## Project Context

- PRD is at `docs/PRD.md`. Read it before starting any work.
- Generated code files (`generated_*.go`) are not hand-edited. Changes to generated code go through `cmd/codegen`.
- The MCP server is HTTP-only (no stdio). This is intentional, not an oversight.
- Token storage default path: `~/.config/spotify-mcp-go/auth/tokens.db`

## Build Commands

See `Makefile` for available targets and usage.
