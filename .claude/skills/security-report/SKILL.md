---
name: security-report
description: Generate a comprehensive security report with executive summary, detailed findings, remediation roadmap, and compliance mapping. Supports markdown and HTML output.
---

# Security Report Skill

## Overview

Generate a comprehensive security assessment report from scan results.

## Inputs

- Scan results (from /scan skill)
- Fix results (from /fix skill, if available)
- Report format: "markdown" (default), "html", or "json"

## Report Sections

### 1. Executive Summary
- Overall security health score (0-100)
- Critical/High finding count
- Top 3 risks with business impact
- Remediation cost estimate (LOC)

### 2. Findings Detail
For each finding:
- Severity badge and OWASP/CWE mapping
- Affected file and line number
- Description of vulnerability
- Proof of concept (how it could be exploited)
- Recommended fix
- Fix status (available/pending/applied)

### 3. Before/After Comparison (if fixes applied)
- Side-by-side diff of vulnerable vs fixed code
- Verification that scanner no longer flags the issue

### 4. Compliance Mapping
- OWASP Top 10 coverage matrix
- OWASP LLM Top 10 coverage (for agent code)
- CWE mapping for each finding

### 5. Remediation Roadmap
- Priority-ordered fix plan
- Estimated effort per fix (LOC)
- Total remediation cost
- Quick wins vs deep fixes

### 6. Dependency Health
- Vulnerable packages list
- Upgrade recommendations
- License compliance issues

## Output

- Markdown report file
- Optional HTML with embedded charts
- Print-ready format for stakeholder distribution

## Guardrails

- Never include actual secrets or credentials in reports
- Redact sensitive file paths if requested
- Include scanner version and rule set for reproducibility
