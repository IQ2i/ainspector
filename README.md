# ainspector

AI-powered code review tool for GitHub Pull Requests and GitLab Merge Requests.

ainspector automatically analyzes your PRs/MRs, extracts modified functions using tree-sitter, and provides AI-generated code reviews as comments.

## Features

- Automatic CI environment detection (GitHub Actions, GitLab CI)
- Function-level analysis using tree-sitter parsing
- Two review modes: `quality` (bugs, readability, project rules) and `security` (vulnerabilities, with a false positive filter)
- Reviews only the changed code, not the entire file
- Native Anthropic API support (prompt caching, structured outputs) and compatibility with any OpenAI-compatible API (OpenAI, Ollama, etc.)
- Smart caching: skips already reviewed functions across commits, even those without any issue
- One summary comment per review mode, kept up to date, with token usage and estimated cost
- Token budget per run, so that a large merge request cannot run up the bill
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
- `--mode` - Review modes to run, comma separated. Defaults to the modes enabled in the configuration (`quality`, plus `security` when `modes.security.enabled` is set). Available modes: `quality`, `security`.
- `--dry-run` - Run the review but print the comments instead of posting them.

**ainspector version** - Print the version number

### Smart Caching

ainspector automatically tracks which functions have been reviewed by embedding a hash marker in review comments. Functions with no issue leave no inline comment, so their hashes are stored in the summary comment. On subsequent runs, it skips functions that haven't changed since the last review, saving API costs and review time.

The hash also covers the version of the review prompt and the project rules: changing either one reviews the functions again.

To force a complete re-review (useful after updating review rules or context):
```bash
./ainspector review --force
```

## LLM Configuration

ainspector calls the Anthropic API directly, or any OpenAI-compatible API. The Anthropic backend is used when `ANTHROPIC_API_KEY` is set, unless `llm.provider` says otherwise.

| Variable | Backend | Required | Default | Description |
|----------|---------|----------|---------|-------------|
| `ANTHROPIC_API_KEY` | Anthropic | Yes | - | API key for the Anthropic API (the model defaults to `claude-sonnet-5-5`) |
| `LLM_API_KEY` | OpenAI-compatible | Yes | - | API key for the LLM service |
| `LLM_BASE_URL` | OpenAI-compatible | No | `https://api.openai.com` | Base URL of the API |
| `LLM_MODEL` | OpenAI-compatible | No | `gpt-4o` | Model name to use |

With the Anthropic backend, the stable part of the prompt (instructions, project context and project rules) is cached, so that the functions of a merge request after the first one read it at a fraction of the price. With an OpenAI-compatible backend, ainspector asks for a JSON reply conforming to a schema and falls back to a less strict mode when the endpoint rejects it.

Each run stops sending requests once `llm.max_tokens_per_run` tokens were consumed (400000 by default; tokens read from the cache do not count). The functions left unreviewed are listed in the summary and reviewed by the next run. The summary also gives the tokens used and an estimated cost, for the models whose price is known (`claude-sonnet-5-5` and `claude-opus-5-5`, others through `llm.prices`).

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
  # Skip whole merge requests
  authors:
    - renovate-bot
  title_patterns:
    - "^\\[skip-review\\]"
  labels:
    - no-ai-review

# Configure project context for better AI understanding
context:
  # Files to include in project context (supports ** for recursive matching)
  include:
    - CLAUDE.md
    - ARCHITECTURE.md
    - docs/**.md
    - "*.config.js"

  # Maximum size of the project context, in bytes (default: 32768)
  max_bytes: 32768

  # Files to exclude (takes priority over include)
  exclude:
    - docs/archive/**
    - "**/*.draft.md"

# LLM backend and limits
llm:
  provider: anthropic          # anthropic or openai (default: anthropic when ANTHROPIC_API_KEY is set)
  max_tokens_per_run: 400000
  prices:                      # USD per million tokens, for models without a built-in price
    my-model: {input: 1.0, output: 4.0}

# Settings of the quality mode and of the security mode (disabled by default)
modes:
  quality:
    enabled: true
    model: gpt-4o
    rules:
      - "No global mutable state"
  security:
    enabled: true
    scope: function+class+related   # function, function+class or function+class+related (default)
    max_findings: 5            # findings commented per run, the others stay in the summary
    hunks:                     # files without functions, reviewed as the changed lines plus context
      extensions: [twig, yaml, yml, vue, env]
      before: 5
      after: 1
    related:                   # files joined to each unit with the scope function+class+related
      presets: [symfony, vue]
      max_files: 6             # per unit
      max_bytes: 40000         # per unit
    false_positive:
      model: claude-opus-5-5   # default on the Anthropic backend
      instructions: "Ignore findings under legacy/"

# Custom review rules enforced by the AI (same as modes.quality.rules)
rules:
  - "All exceptions must be logged before rethrowing"
  - "Database queries must use prepared statements"
  - "No console.log allowed in production code"
  - "All public functions must have JSDoc comments"
  - "API responses must include proper error codes"
```

### Configuration Options

**ignore.paths** - Glob patterns for files to skip during review. Useful for excluding vendor code, generated files, and test files.

**ignore.authors**, **ignore.title_patterns**, **ignore.labels** - Skip the whole merge request when its author or one of its labels matches (case-insensitive), or when its title matches one of the regular expressions. Handy for dependency bots.

**modes.quality** - `enabled` (default `true`), `model` (default: the model of the LLM backend) and `rules`, which are added after the top-level `rules`.

**modes.security** - Disabled unless `enabled: true`. `scope` is the code given with each unit: `function`, `function+class` (adds the header of the class) or `function+class+related` (the default, adds the related files). `max_findings` caps the findings commented per run (default `5`). `hunks` tells which files without functions are reviewed, and how many lines of context surround each change (defaults above); lock files (`*.lock`, `package-lock.json`, `pnpm-lock.yaml`) and minified assets are never reviewed that way. `false_positive.model` is the model of the second pass (`claude-opus-5-5` on the Anthropic backend, the default model of the backend otherwise), `false_positive.instructions` is appended to the built-in instructions. `model` is the model of the review itself.

**context.include** - Files to include when generating project context. The AI will read these files to better understand your project's architecture, conventions, and requirements. By default, ainspector automatically searches for common documentation files like `CLAUDE.md`, `AGENTS.md`, `.cursorrules`, `README.md`, and `.github/copilot-instructions.md`.

**context.exclude** - Files to exclude from project context (overrides include patterns).

**context.max_bytes** - Size cap of the project context (default `32768`). Files are added in order until the cap is reached: the one that crosses it is truncated, the following ones are left out, and the summary says so.

**llm** - `provider` (`anthropic` or `openai`), `max_tokens_per_run` and `prices`, see [LLM Configuration](#llm-configuration).

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

These files are given as they are to the AI, within the `context.max_bytes` limit, so that it knows your project's architecture, coding conventions, and requirements before reviewing your code. Without `context.include`, the dependency manifests (`go.mod`, `package.json`, `composer.json`...) are given too, after the documentation files.

### Review Summary

After each review, ainspector posts one summary comment per mode and updates it on the next runs instead of adding another one. It tells how many functions (or units, for the security mode) were reviewed and how many issues were reported, the findings left out by the cap and those discarded as false positives, the tokens used, the estimated cost, the functions that could not be reviewed (token budget reached, failed requests) and what was left out of the project context. It also carries the hidden state that lets later runs skip the functions already reviewed. Only a summary written by the account of the token is trusted.

### Security Review

The `security` mode looks only for vulnerabilities in the code of the merge request: missing or inconsistent access control, insecure direct object references, injection, XSS, CSRF, mass assignment, data exposure, unsafe file uploads, secrets, weak cryptography, unsafe deserialization, denial of service. Every finding comes with a concrete exploitation scenario, a remediation and a confidence level. The mode also reports, with the `low` severity, a protected route whose merge request adds no test of the denied access.

What it reviews, beyond the functions with added lines:

- Functions whose only change is a deletion: a removed `#[IsGranted]`, authorization check or escaping is a signal.
- Changes outside any function (class attributes, imports, properties).
- Files without functions (`twig`, `yaml`, `yml`, `vue`, `env` by default): the changed lines with a few lines of context.
- With `function+class` and above, the header of the enclosing class: imports, class attributes, properties, constructor and the signature of the other methods, which a controller cannot be judged without. This is available for PHP, TypeScript, JavaScript, Python, Java, C# and Ruby. Go, Rust, C, C++ and Bash have no class header and stay at the `function` scope. A file that was deleted is not reviewed.
- With `function+class+related`, the files related to the unit, which tell whether an access check really protects it. They are found by naming conventions and text search in the checkout of the job, never by static analysis:

  | Changed file | Files joined |
  |---|---|
  | Symfony controller (`src/Controller/`) | the voters named after the entities it receives or handling the attributes it checks, the `firewalls` and `access_control` of `config/packages/security.yaml`, the Twig templates it renders |
  | Entity (`src/Entity/`) | its repository, its serializer configuration when it has no `#[Groups]` |
  | Form (`src/Form/`) | the entity of its `data_class` |
  | Voter (`*Voter.php`) | the controllers that use it |
  | Twig template (`templates/`) | the code that renders it |
  | Vue component | the files importing it, the stores and composables it imports |

  A unit gets at most `related.max_files` files and `related.max_bytes` bytes: the rest is left out or truncated, and the summary says so. Nothing is joined when no rule finds anything. `vendor/`, `node_modules/`, `var/`, `.env*` files and the paths of `ignore.paths` are never read. The files come from the checkout of the job: when it is not the head of the merge request (merge result pipeline), the run logs the difference. Changing a related file reviews the units that use it again.

To keep the noise low, findings of `info` severity or under 50% confidence are dropped, findings already reported by a previous run are not repeated (even if their line moved), and a second call to a stronger model discards the false positives. That filter never discards denial of service or resource exhaustion findings. If the filter fails, the findings are published unfiltered and the summary says so. Only the `max_findings` most severe findings are commented, the others are listed in the summary. When nothing is found, no inline comment is posted: only the summary.

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
