# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

```bash
make build       # Build binary to bin/ainspector
make test        # Run all tests with verbose output
make lint        # Run golangci-lint
make fmt         # Format code
make deps        # Tidy go modules
make clean       # Remove bin/ directory
make run         # Build and run

# Run a single test
go test -v -run TestName ./internal/package/...

# Run tests for a specific package
go test -v ./internal/config/...

# Run tests with race detection
go test -v -race ./...

# Run tests with coverage
go test -v -coverprofile=coverage.out ./...
```

## Project Structure

```
ainspector/
├── main.go                 # Entry point - calls cmd.Execute()
├── go.mod                  # Go 1.24.0, module github.com/iq2i/ainspector
├── Makefile
├── cmd/
│   └── root.go             # Cobra CLI: review, version commands
└── internal/
    ├── ci/                 # CI environment detection
    │   ├── detect.go       # Detect(), detectGitHub(), detectGitLab()
    │   └── detect_test.go
    ├── config/             # Configuration file handling
    │   ├── config.go       # Load(), LoadFromPath(), Config struct
    │   ├── filter.go       # ShouldIgnore() with glob patterns
    │   └── config_test.go
    ├── provider/           # Git hosting provider abstraction
    │   ├── provider.go     # Provider interface, ModifiedFile struct
    │   ├── github.go       # GitHubProvider implementation
    │   ├── gitlab.go       # GitLabProvider implementation
    │   └── provider_test.go
    ├── diff/               # Unified diff parsing
    │   ├── parser.go       # ParsePatch(), ModifiedLines struct
    │   └── parser_test.go
    ├── parser/             # Tree-sitter code parsing
    │   ├── parser.go       # Parse(), Function struct, language configs
    │   └── parser_test.go
    ├── extractor/          # Orchestration layer
    │   ├── extractor.go    # ExtractModifiedFunctions(), ExtractedFunction
    │   └── extractor_test.go
    └── llm/                # LLM integration
        ├── client.go       # Client struct, Complete()
        ├── review.go       # ReviewFunctions(), ReviewResult
        ├── client_test.go
        └── review_test.go
```

## Architecture Overview

ainspector is an AI-powered code review tool for GitHub PRs and GitLab MRs. It extracts modified functions from diffs and sends them to an LLM for review.

### Review Pipeline

```
CI Detection → Provider API → File Collection → Function Extraction → LLM Review → Post Comment
```

### Data Flow

```
GitHub/GitLab PR/MR
    ↓ (ci.Detect)
Environment{Provider, Owner, Repo, PRNumber, Token}
    ↓ (provider.GetModifiedFiles)
[]ModifiedFile{Path, Patch, Status}
    ↓ (diff.ParsePatch)
ModifiedLines{Added, Deleted}
    ↓ (parser.Parse)
[]Function{Name, StartLine, EndLine, Content}
    ↓ (extractor.ExtractModifiedFunctions)
[]ExtractedFunction{Name, Content, Diff, FilePath}
    ↓ (llm.ReviewFunctions)
[]ReviewResult{Function, Review, Error}
    ↓ (provider.PostComment)
Comment on PR/MR
```

## Key Types & Interfaces

### Provider Interface (`internal/provider/provider.go`)

```go
type Provider interface {
    GetModifiedFiles(ctx context.Context, number int) ([]ModifiedFile, error)
    GetFileContent(ctx context.Context, path string) (string, error)
    PostComment(ctx context.Context, number int, body string) error
    CreateReview(ctx context.Context, number int, comments []ReviewComment) error
}

type ModifiedFile struct {
    Path       string  // File path
    OldPath    string  // For renames
    Status     string  // "added", "modified", "deleted", "renamed"
    Patch      string  // Unified diff
}

type ReviewComment struct {
    Path       string  // File path
    Line       int     // Line number
    Body       string  // Comment body
    Suggestion string  // Optional: code suggestion
}
```

### CI Environment (`internal/ci/detect.go`)

```go
type Environment struct {
    Provider   string  // "github" or "gitlab"
    Owner      string
    Repo       string
    PRNumber   int
    Token      string
    ServerHost string  // For self-hosted GitLab
}
```

### Config (`internal/config/config.go`)

```go
type Config struct {
    Ignore IgnoreConfig `yaml:"ignore"`
}

type IgnoreConfig struct {
    Paths []string `yaml:"paths"`  // Glob patterns (supports **)
}
```

### Parser (`internal/parser/parser.go`)

```go
type Function struct {
    Name      string
    StartLine int
    EndLine   int
    Content   string
}
```

### Extractor (`internal/extractor/extractor.go`)

```go
type ExtractedFunction struct {
    Name       string
    StartLine  int
    EndLine    int
    Content    string  // Full function code
    Diff       string  // Function-specific diff
    FilePath   string
    Language   string
    ChangeType string  // "added", "modified"
}
```

### LLM (`internal/llm/`)

```go
type Client struct {
    baseURL    string
    apiKey     string
    model      string
    httpClient *http.Client
}

type Suggestion struct {
    Line        int    `json:"line"`        // Line number in file
    Description string `json:"description"` // Issue description
    Code        string `json:"suggestion"`  // Suggested fix
}

type ReviewResult struct {
    Function    ExtractedFunction
    Suggestions []Suggestion  // Parsed from LLM JSON response
    RawReview   string        // Original LLM response
    Error       error
}
```

The LLM returns either "LGTM" (no issues) or a JSON object:
```json
{
  "issues": [
    {"line": 10, "description": "Bug description", "suggestion": "fixed code"}
  ]
}
```

## Package Responsibilities

| Package | Responsibility |
|---------|---------------|
| `cmd` | Cobra CLI, orchestrates the review flow in `runReview()` |
| `ci` | Detects GitHub Actions or GitLab CI from environment variables |
| `provider` | Abstracts GitHub/GitLab APIs (fetch files, post comments) |
| `config` | Loads `ainspector.yaml`, checks file ignore patterns |
| `diff` | Parses unified diffs, extracts modified line numbers |
| `parser` | Tree-sitter parsing, extracts functions from source code |
| `extractor` | Orchestrates: skip ignored files, parse, filter modified functions |
| `llm` | OpenAI-compatible API client, sends functions for review |

## Supported Languages

Tree-sitter parsers with function extraction queries:
- Go (`.go`)
- JavaScript/JSX (`.js`, `.jsx`)
- TypeScript/TSX (`.ts`, `.tsx`)
- Python (`.py`)
- Rust (`.rs`)
- Java (`.java`)
- C/C++ (`.c`, `.h`, `.cpp`, `.cc`, `.hpp`)
- C# (`.cs`)
- PHP (`.php`)
- Ruby (`.rb`)
- Bash (`.sh`, `.bash`)

## Dependencies

**Core:**
- `github.com/google/go-github/v57` - GitHub API
- `gitlab.com/gitlab-org/api/client-go` - GitLab API
- `github.com/spf13/cobra` - CLI framework
- `github.com/sourcegraph/go-diff` - Diff parsing

**Tree-sitter:** Multiple `github.com/tree-sitter-grammars/*` packages

**Utilities:**
- `github.com/bmatcuk/doublestar/v4` - Glob pattern matching
- `gopkg.in/yaml.v3` - YAML parsing

## Configuration

Optional `ainspector.yaml` at project root:

```yaml
ignore:
  paths:
    - vendor/           # Directory patterns
    - node_modules/
    - "**/*_test.go"    # Glob with ** for recursive
    - "*.generated.go"  # Filename patterns
```

## Environment Variables

**LLM (required):**
- `LLM_API_KEY` - API key for LLM service

**LLM (optional):**
- `LLM_BASE_URL` - Default: `https://api.openai.com`
- `LLM_MODEL` - Default: `gpt-4o`

**GitHub Actions (auto-provided):**
- `GITHUB_TOKEN`, `GITHUB_REPOSITORY`, `GITHUB_REF`, `GITHUB_EVENT_PATH`

**GitLab CI (auto-provided):**
- `GITLAB_TOKEN` or `CI_JOB_TOKEN`
- `CI_PROJECT_PATH`, `CI_MERGE_REQUEST_IID`, `CI_SERVER_HOST`

## Testing Patterns

- All packages have `*_test.go` files
- Table-driven tests with `[]struct{name, input, expected}`
- Use `t.TempDir()` for temporary files
- Provider tests use mock HTTP servers
- LLM tests use `httptest.NewServer`

## Common Development Tasks

**Add a new language:**
1. Add tree-sitter grammar dependency to `go.mod`
2. Add `LanguageConfig` in `internal/parser/parser.go`
3. Define tree-sitter query for function extraction
4. Add tests in `parser_test.go`

**Add a new provider:**
1. Implement `Provider` interface in `internal/provider/`
2. Add detection logic in `internal/ci/detect.go`
3. Update `cmd/root.go` to handle new provider type

**Modify ignore patterns:**
1. Update `IgnoreConfig` struct in `internal/config/config.go`
2. Update `ShouldIgnore()` in `internal/config/filter.go`
3. Update tests in `config_test.go`
