---
name: code-review
description: Reviews code changes for bugs, security issues, and quality problems
---

# Code Review Skill

Review code changes and identify bugs, security issues, and quality problems.

## Workflow

1. **Get the code changes** - Use the method provided in the prompt, or if none
   specified:
   - For a PR: `gh pr diff <PR_NUMBER>`
   - For local changes: `git diff main` or `git diff --staged`

2. **Read full files and related code** before commenting - verify issues exist
   and consider how similar code is implemented elsewhere in the codebase

3. **Analyze for issues** - Focus on what could break production

4. **Report findings** - Use the method provided in the prompt, or summarize
   directly

## Severity Levels

- **CRITICAL**: Security vulnerabilities, auth bypass, data corruption, crashes
- **IMPORTANT**: Logic bugs, race conditions, resource leaks, unhandled errors
- **NITPICK**: Minor improvements, style issues, portability concerns

## What to Look For

- **Security**: Auth bypass, injection, data exposure, improper access control
- **Correctness**: Logic errors, off-by-one, nil/null handling, error paths
- **Concurrency**: Race conditions, deadlocks, missing synchronization
- **Resources**: Leaks, unclosed handles, missing cleanup
- **Error handling**: Swallowed errors, missing validation, panic paths

## What NOT to Comment On

- Style that matches existing project patterns
- Code that already exists unchanged
- Theoretical issues without concrete impact
- Changes unrelated to the PR's purpose

## Review Quality

- Explain **impact** ("causes crash when X" not "could be better")
- Make observations **actionable** with specific fixes
- Read the **full context** before commenting on a line
- Check project guidelines for conventions before flagging style

## Comment Standards

- **Only comment when confident** - If you're not 80%+ sure it's a real issue,
  don't comment. Verify claims before posting.
- **No speculation** - Avoid "might", "could", "consider". State facts or skip.
- **Verify technical claims** - Check documentation or code before asserting how
  something works. Don't guess at API behavior or syntax rules.
