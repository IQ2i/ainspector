# ainspector

AI-powered code review tool for GitHub Pull Requests and GitLab Merge Requests.

ainspector automatically analyzes your PRs/MRs, extracts modified functions using tree-sitter, and provides AI-generated code reviews as comments.

## Features

- Automatic CI environment detection (GitHub Actions, GitLab CI)
- Function-level analysis using tree-sitter parsing
- Reviews only the changed code, not the entire file
- Compatible with any OpenAI-compatible API (OpenAI, Anthropic, Ollama, etc.)
- Configurable file exclusion patterns

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
  variables:
    GITLAB_TOKEN: $GITLAB_API_TOKEN  # or use CI_JOB_TOKEN with appropriate permissions
    LLM_API_KEY: $LLM_API_KEY
    LLM_BASE_URL: https://api.openai.com
    LLM_MODEL: gpt-4o
```

## LLM Configuration

ainspector works with any OpenAI-compatible API. Configure it using environment variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `LLM_API_KEY` | Yes | - | API key for the LLM service |
| `LLM_BASE_URL` | No | `https://api.openai.com` | Base URL of the API |
| `LLM_MODEL` | No | `gpt-4o` | Model name to use |

### Provider Examples

**OpenAI**
```bash
LLM_API_KEY=sk-...
LLM_BASE_URL=https://api.openai.com
LLM_MODEL=gpt-4o
```

**Anthropic (via OpenAI-compatible proxy)**
```bash
LLM_API_KEY=sk-ant-...
LLM_BASE_URL=https://api.anthropic.com
LLM_MODEL=claude-sonnet-4-20250514
```

**Azure OpenAI**
```bash
LLM_API_KEY=your-azure-key
LLM_BASE_URL=https://your-resource.openai.azure.com
LLM_MODEL=your-deployment-name
```

**Ollama (local)**
```bash
LLM_API_KEY=ollama  # any non-empty value
LLM_BASE_URL=http://localhost:11434
LLM_MODEL=llama3
```

**OpenRouter**
```bash
LLM_API_KEY=sk-or-...
LLM_BASE_URL=https://openrouter.ai/api
LLM_MODEL=anthropic/claude-sonnet-4-20250514
```

## Configuration File

Create an `ainspector.yaml` (or `ainspector.yml`) at the root of your repository to exclude files from review:

```yaml
ignore:
  # Glob patterns (supports ** for recursive matching)
  paths:
    - vendor/
    - node_modules/
    - "**/*_test.go"
    - "*.generated.go"
    - dist/
```

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
