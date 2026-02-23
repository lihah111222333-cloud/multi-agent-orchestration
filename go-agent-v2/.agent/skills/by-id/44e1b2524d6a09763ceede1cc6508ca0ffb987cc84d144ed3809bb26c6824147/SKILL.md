---
name: github-issues
description: Create, update, and manage GitHub issues using MCP tools. Use this skill when users want to create bug reports, feature requests, or task issues, update existing issues, add labels/assignees/milestones, or manage issue workflows.
tags: [github, issues, bug-report, feature-request, project-management]
---

# GitHub Issues

Manage GitHub issues using the `@modelcontextprotocol/server-github` MCP server.

## When to Use This Skill

Triggers on requests like:
- "create an issue"
- "file a bug"
- "request a feature"
- "update issue #X"
- "add label to issue"
- "assign issue to"
- Any GitHub issue management task

---

## Available MCP Tools

| Tool | Purpose |
|------|---------|
| `mcp__github__create_issue` | Create new issues |
| `mcp__github__update_issue` | Update existing issues |
| `mcp__github__get_issue` | Fetch issue details |
| `mcp__github__search_issues` | Search issues |
| `mcp__github__add_issue_comment` | Add comments |
| `mcp__github__list_issues` | List repository issues |

---

## Workflow

1. **Determine action**: Create, update, or query?
2. **Gather context**: Get repo info, existing labels, milestones if needed
3. **Structure content**: Use appropriate template
4. **Execute**: Call the appropriate MCP tool
5. **Confirm**: Report the issue URL to user

---

## Creating Issues

### Required Parameters

| Parameter | Description |
|-----------|-------------|
| `owner` | Repository owner (user or organization) |
| `repo` | Repository name |
| `title` | Issue title (clear and descriptive) |
| `body` | Issue body (markdown formatted) |

### Optional Parameters

| Parameter | Description |
|-----------|-------------|
| `labels` | Array of label names |
| `assignees` | Array of GitHub usernames |
| `milestone` | Milestone number |

### Title Guidelines

- Start with component/area in brackets: `[auth] Login fails on Safari`
- Be specific: "Button color wrong" → "[UI] Submit button uses wrong shade of blue (#3498db instead of #2980b9)"
- Include error codes if applicable: `[API] 500 error on /users endpoint`

### Body Structure

```markdown
## Description
[Clear description of the issue]

## Steps to Reproduce (for bugs)
1. Step one
2. Step two
3. Step three

## Expected Behavior
[What should happen]

## Actual Behavior
[What actually happens]

## Environment (if applicable)
- OS: 
- Browser: 
- Version: 

## Additional Context
[Screenshots, logs, related issues]
```

---

## Updating Issues

Use `mcp__github__update_issue` with:

| Parameter | Description |
|-----------|-------------|
| `owner` | Repository owner |
| `repo` | Repository name |
| `issue_number` | The issue number to update |
| `title` | New title (optional) |
| `body` | New body (optional) |
| `state` | `open` or `closed` |
| `labels` | Replace all labels |
| `assignees` | Replace all assignees |

---

## Examples

### Example 1: Bug Report

```
User: "Create a bug report for the login page not working on mobile"

Action: Call mcp__github__create_issue with:
{
  "owner": "username",
  "repo": "project",
  "title": "[Mobile] Login page not responsive on small screens",
  "body": "## Description\nThe login page does not render correctly on mobile devices...",
  "labels": ["bug", "mobile", "priority:high"]
}
```

### Example 2: Feature Request

```
User: "Request a dark mode feature"

Action: Call mcp__github__create_issue with:
{
  "owner": "username",
  "repo": "project",
  "title": "[Feature] Add dark mode support",
  "body": "## Feature Request\n\n### Problem\nThe current light theme is hard on the eyes...",
  "labels": ["enhancement", "ui/ux"]
}
```

---

## Common Labels

| Label | Usage |
|-------|-------|
| `bug` | Something isn't working |
| `enhancement` | New feature or request |
| `documentation` | Documentation improvements |
| `good first issue` | Good for newcomers |
| `help wanted` | Extra attention needed |
| `priority:high` | High priority |
| `priority:low` | Low priority |
| `wontfix` | Will not be addressed |

---

## Tips

1. **Always confirm repository**: Ask for owner/repo if not clear from context
2. **Check existing issues**: Search before creating duplicates
3. **Use templates**: If the repo has issue templates, follow them
4. **Be descriptive**: More context = faster resolution
5. **Add labels appropriately**: Help with issue triage

---

## ⚠️ 强制输出 Token 空间

> **重要规则**：使用此技能时，必须在每次重要输出前检查上下文空间。

### 输出格式

```
📊 剩余上下文空间: ~{百分比}%
```
