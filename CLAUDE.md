# Development Rules

## Development Cycle

Every issue follows this uninterrupted flow. Do not stop or ask for confirmation between steps, except where noted.

1. `/linear-session` to pick or confirm the issue
2. **Check working tree**: run `git status`. If there are uncommitted or untracked changes, stop and ask the user how to handle them before continuing. Do not stash, commit, or discard without explicit instruction.
3. `linear issue start <ID>` to create branch and move to In Progress
4. Read the PRD and issue acceptance criteria
5. Implement (TDD, see below). Run `go mod tidy` after adding new imports.
6. `/verify-acceptance` to verify criteria, check boxes, and post evidence
7. Commit, push, create PR
8. Merge PR: `gh pr merge <N> --squash --delete-branch`
9. Return to main: `git checkout main && git pull`
10. Mark done: `linear issue update <ID> --state Done`
11. Return to step 1 for the next issue

When acceptance criteria pass, the work is done. Commit and land immediately. The only reason to pause is a failure (tests, build, push, merge).

## Linear Integration

This project uses the Linear CLI for issue management. Team `SPO` in the `make-something` workspace. PRs are created via `linear issue pr` (falls back to `gh pr create` if that fails).

## TDD

Write tests first, then implementation. No exceptions.

1. Write a COMPLETE (not stubbed) failing test for the behaviour you're about to implement
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

## Project Context

- PRD is at `docs/PRD.md`. Read it before starting any work.
- Generated code files (`generated_*.go`) are not hand-edited. Changes to generated code go through `cmd/codegen`.
- The MCP server is HTTP-only (no stdio). This is intentional, not an oversight.
- Token storage default path: `~/.config/spotify-mcp-go/auth/tokens.db`

## Go Conventions

- After adding a new import that introduces a dependency, run `go mod tidy` before running tests.
- Run `go vet ./...` before committing.

## CLI Reference

### Linear CLI

- List issues: `linear issue list --team SPO --state <state> --all-assignees`
  States: `triage`, `backlog`, `unstarted`, `started`, `completed`, `canceled`
- View issue: `linear issue view <ID>`
- Start issue: `linear issue start <ID>`
- Update state: `linear issue update <ID> --state <State>`
- Create PR: `linear issue pr <ID>`
- Comment: `linear issue comment add <ID> --body "<text>"`
  For long/markdown bodies: `linear issue comment add <ID> --body-file <path>`

### GitHub CLI

- Merge PR: `gh pr merge <number> --squash --delete-branch`
- Create PR: `gh pr create --title "..." --body "..."`

## Build Commands

See `Makefile` for available targets and usage.
