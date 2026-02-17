---
name: scan
description: Run a comprehensive security scan on a codebase using available security scanners (Semgrep, Bandit, Trivy, osv-scanner). Returns structured findings with severity, file location, and OWASP mapping.
---

# Security Scan Skill

## Overview

Run a multi-engine security scan on a target directory or repo and produce a structured vulnerability report.

## Inputs

- Target: directory path, GitHub repo URL, or "." for current directory
- Scope: "full" (all engines) or specific engine name

## Engines

1. **Semgrep** - SAST rules for Python/JS/TS/Go
2. **Trivy** - Dependency + container + IaC scanning
3. **osv-scanner** - Open-source vulnerability database lookup
4. **Bandit** - Python-specific security linting (if Python code present)

## Workflow

1. Detect project languages and package managers
2. Run applicable scanners in parallel
3. Deduplicate findings across engines
4. Classify by severity: CRITICAL, HIGH, MEDIUM, LOW
5. Map findings to OWASP Top 10 / OWASP LLM Top 10
6. Output structured JSON report + human-readable summary

## Output Format

```json
{
  "scan_id": "uuid",
  "target": "path/or/url",
  "timestamp": "ISO8601",
  "summary": {
    "critical": 0,
    "high": 2,
    "medium": 5,
    "low": 12,
    "total": 19
  },
  "findings": [
    {
      "id": "finding-uuid",
      "engine": "semgrep",
      "rule": "typescript.express.security.audit.xss",
      "severity": "HIGH",
      "file": "src/api/handler.ts",
      "line": 42,
      "title": "Cross-Site Scripting (XSS)",
      "description": "User input rendered without sanitization",
      "owasp": "A03:2021 Injection",
      "fix_available": true,
      "estimated_loc": 3
    }
  ]
}
```

## Guardrails

- Read-only: do not modify any scanned files
- Do not execute untrusted code from scanned repos
- Filter out low-confidence results by default
- Report scanner errors separately from findings
