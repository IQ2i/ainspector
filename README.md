# ainspector

AI-powered code review tool for GitHub Pull Requests and GitLab Merge Requests.

ainspector automatically analyzes your PRs/MRs, extracts modified functions using tree-sitter, and provides AI-generated code reviews as comments.

## Features

- Automatic CI environment detection (GitHub Actions, GitLab CI)
- Function-level analysis using tree-sitter parsing
- Reviews only the changed code, not the entire file
- Compatible with any OpenAI-compatible API (OpenAI, Anthropic, Ollama, etc.)
- Smart caching: skips already reviewed functions across commits
- Project context generation from documentation files (CLAUDE.md, README.md, etc.)
- Custom review rules enforcement
- Configurable context files and exclusion patterns
- Language-specific review guidelines

### Supported Languages

Go, JavaScript, TypeScript, Python, Rust, Java, C, C++, C#, PHP, Ruby, Bash

## Installation

Download the latest binary from the [releases page](https://github.com/iq2i/ainspector/releases) or build from source:

```bash
git clone https://github.com/iq2i/ainspector.git
cd ainspector
make build
```

## Usage

### GitHub Actions

Create `.github/workflows/ainspector.yml`:

```yaml
name: AI Code Review

on:
  pull_request:
    types: [opened, synchronize]

jobs:
  review:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
    steps:
      - uses: actions/checkout@v4

      - name: Download ainspector
        run: |
          curl -sL https://github.com/iq2i/ainspector/releases/latest/download/ainspector-linux-amd64 -o ainspector
          chmod +x ainspector

      - name: Run AI review
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          LLM_API_KEY: ${{ secrets.LLM_API_KEY }}
          LLM_BASE_URL: https://api.openai.com  # optional
          LLM_MODEL: gpt-4o                      # optional
        run: ./ainspector review
        # Use --force to re-review all functions (ignores cache)
        # run: ./ainspector review --force
```

### GitLab CI

Add to your `.gitlab-ci.yml`:

```yaml
ai-review:
  stage: test
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
  script:
    - curl -sL https://github.com/iq2i/ainspector/releases/latest/download/ainspector-linux-amd64 -o ainspector
    - chmod +x ainspector
    - ./ainspector review
    # Use --force to re-review all functions (ignores cache)
    # - ./ainspector review --force
  variables:
    GITLAB_TOKEN: $GITLAB_API_TOKEN  # or use CI_JOB_TOKEN with appropriate permissions
    LLM_API_KEY: $LLM_API_KEY
    LLM_BASE_URL: https://api.openai.com
    LLM_MODEL: gpt-4o
```

### Command Line Options

**ainspector review** - Run code review on the current PR/MR

Options:
- `--force`, `-f` - Force re-review of all functions, ignoring the cache. By default, ainspector skips functions that have already been reviewed in previous runs.

**ainspector version** - Print the version number

### Smart Caching

ainspector automatically tracks which functions have been reviewed by embedding a hash marker in review comments. On subsequent runs, it skips functions that haven't changed since the last review, saving API costs and review time.

To force a complete re-review (useful after updating review rules or context):
```bash
./ainspector review --force
```

## LLM Configuration

ainspector works with any OpenAI-compatible API. Configure it using environment variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `LLM_API_KEY` | Yes | - | API key for the LLM service |
| `LLM_BASE_URL` | No | `https://api.openai.com` | Base URL of the API |
| `LLM_MODEL` | No | `gpt-4o` | Model name to use |

## Configuration File

Create an `ainspector.yaml` (or `ainspector.yml`) at the root of your repository to customize the review behavior:

```yaml
# Exclude files from code review
ignore:
  paths:
    - vendor/
    - node_modules/
    - "**/*_test.go"
    - "*.generated.go"
    - dist/

# Configure project context for better AI understanding
context:
  # Files to include in project context (supports ** for recursive matching)
  include:
    - CLAUDE.md
    - ARCHITECTURE.md
    - docs/**.md
    - "*.config.js"

  # Files to exclude (takes priority over include)
  exclude:
    - docs/archive/**
    - "**/*.draft.md"

# Custom review rules enforced by the AI
rules:
  - "All exceptions must be logged before rethrowing"
  - "Database queries must use prepared statements"
  - "No console.log allowed in production code"
  - "All public functions must have JSDoc comments"
  - "API responses must include proper error codes"
```

### Configuration Options

**ignore.paths** - Glob patterns for files to skip during review. Useful for excluding vendor code, generated files, and test files.

**context.include** - Files to include when generating project context. The AI will read these files to better understand your project's architecture, conventions, and requirements. By default, ainspector automatically searches for common documentation files like `CLAUDE.md`, `AGENTS.md`, `.cursorrules`, `README.md`, and `.github/copilot-instructions.md`.

**context.exclude** - Files to exclude from project context (overrides include patterns).

**rules** - Custom project-specific review rules. These rules are enforced by the AI reviewer and any violations will be explicitly reported in the code review comments.

## How It Works

### Project Context Generation

Before reviewing code, ainspector generates project context to help the AI understand your codebase better. The context is built from:

1. **Automatic Discovery**: Common documentation files are automatically detected:
   - `CLAUDE.md` - Claude-specific project instructions
   - `AGENTS.md` - Agent configuration and guidelines
   - `.cursorrules` - Cursor IDE AI rules
   - `.github/copilot-instructions.md` - GitHub Copilot instructions
   - `README.md` - Project overview and documentation

2. **Custom Context Files**: Files specified in `context.include` in your `ainspector.yaml`

The AI analyzes these files to understand your project's architecture, coding conventions, and requirements before reviewing your code.

### Language-Specific Review Guidelines

ainspector includes tailored review guidelines for each supported language:

- **Go**: Error handling, nil checks, goroutine leaks, defer usage
- **JavaScript/TypeScript**: Promise handling, type safety, async/await patterns
- **Python**: Exception handling, type hints, resource management
- **Rust**: Ownership rules, unsafe blocks, error propagation
- **Java**: Exception handling, resource leaks, null safety
- **C/C++**: Memory management, buffer overflows, pointer safety
- **C#**: Disposal patterns, async/await, null reference handling
- **PHP**: SQL injection, XSS prevention, type declarations
- **Ruby**: Exception handling, nil checks, block usage
- **Bash**: Quoting, error handling, shellcheck compliance

## Environment Variables Reference

### GitHub Actions

| Variable | Description |
|----------|-------------|
| `GITHUB_TOKEN` | GitHub API token (automatically provided) |
| `GITHUB_REPOSITORY` | Repository in `owner/repo` format (automatic) |
| `GITHUB_REF` | Git ref for the PR (automatic) |

### GitLab CI

| Variable | Description |
|----------|-------------|
| `GITLAB_TOKEN` | GitLab API token with `api` scope |
| `CI_JOB_TOKEN` | Alternative to GITLAB_TOKEN (limited permissions) |
| `CI_PROJECT_PATH` | Project path (automatic) |
| `CI_MERGE_REQUEST_IID` | Merge request ID (automatic) |
| `CI_SERVER_HOST` | GitLab host for self-hosted instances |

## License

MIT
