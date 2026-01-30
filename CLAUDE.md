# CLAUDE.md

Guide for working with ainspector - an AI-powered code review tool for GitHub PRs and GitLab MRs.

## Quick Start

```bash
make build    # Build binary
make test     # Run tests
make lint     # Lint code
```

## Architecture

**Pipeline:** CI Detection → Provider API → Function Extraction → LLM Review → Post Comment

**Key Packages:**
- `cmd/` - CLI entry point
- `internal/ci/` - Detect GitHub/GitLab CI environment
- `internal/provider/` - GitHub/GitLab API abstraction
- `internal/config/` - Load `ainspector.yaml` config
- `internal/diff/` - Parse unified diffs
- `internal/parser/` - Tree-sitter function extraction
- `internal/extractor/` - Orchestrate parsing and filtering
- `internal/llm/` - OpenAI-compatible API client

## Configuration

`ainspector.yaml` (optional):
```yaml
ignore:
  paths:
    - vendor/
    - "**/*_test.go"
```

## Environment Variables

**Required:**
- `LLM_API_KEY` - API key for LLM

**Optional:**
- `LLM_BASE_URL` - Default: `https://api.openai.com`
- `LLM_MODEL` - Default: `gpt-4o`

**Auto-provided by CI:**
- GitHub: `GITHUB_TOKEN`, `GITHUB_REPOSITORY`, `GITHUB_REF`, `GITHUB_EVENT_PATH`
- GitLab: `GITLAB_TOKEN`, `CI_PROJECT_PATH`, `CI_MERGE_REQUEST_IID`

## Supported Languages

Go, JavaScript/JSX, TypeScript/TSX, Python, Rust, Java, C/C++, C#, PHP, Ruby, Bash

## Code Guidelines

**IMPORTANT**: Follow these guidelines strictly:
- **No documentation**: Do not create or update documentation files (README, docs, etc.) unless explicitly requested
- **Minimal comments**: Do not add code comments unless the logic is complex or the user explicitly requests them
- **No emojis**: Never use emojis in responses or code
- **Concise responses**: Provide direct, concise answers with only essential and relevant information
