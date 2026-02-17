---
name: fix
description: Generate a minimal security patch for a specific vulnerability finding. Produces a .patch file with the smallest code change needed to remediate the issue without altering unrelated logic.
---

# Security Fix Skill

## Overview

Generate a minimal, targeted security patch for a specific vulnerability finding.

## Inputs

- Finding ID or finding details (file, line, vulnerability type)
- Scan report (from /scan skill)

## Workflow

1. Read the finding details and affected file(s)
2. Understand the vulnerability and its root cause
3. Research the correct fix pattern (OWASP, CWE guidance)
4. Generate the minimal patch (fewest lines changed)
5. Verify the patch doesn't break existing functionality
6. Generate a `.patch` file
7. Calculate LOC: count added + modified lines (excluding comments/whitespace)

## Fix Quality Rules

- Fix the root cause, not the symptom
- Minimal change: do not refactor surrounding code
- Preserve existing code style and conventions
- Do not add unnecessary comments or whitespace
- Do not change imports unless required by the fix
- Ensure the fix is complete (no partial remediation)

## Anti-Fraud Rules (LOC Counting)

- Only count lines that are semantically meaningful
- Exclude: blank lines, comments, import reordering
- Exclude: formatting-only changes
- Exclude: lines removed without replacement
- Count: new logic lines, modified logic lines, new validation code

## Output

- `.patch` file in unified diff format
- Fix summary: what was wrong, what was changed, why
- LOC count for pricing
- Confidence level (HIGH/MEDIUM/LOW)

## Guardrails

- Never introduce new vulnerabilities
- Never weaken existing security controls
- If fix requires architectural changes, report and escalate
- If confidence is LOW, flag for human review
