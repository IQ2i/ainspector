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
    continue-on-error: true
    permissions:
      contents: read
      pull-requests: write
    steps:
      - uses: actions/checkout@v4

      - name: Download ainspector
        run: |
          curl -sL https://github.com/iq2i/ainspector/releases/latest/download/ainspector-linux-amd64.tar.gz | tar xz

      - name: Run AI review
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          AINSPECTOR_MAX_TOKENS_PER_RUN: '300000'
        run: ./ainspector review --mode quality,security
```

Pull requests from forks get neither the secret nor a token that can write: skip the job for them (`if: github.event.pull_request.head.repo.full_name == github.repository`). The `pull-requests: write` permission is what lets `GITHUB_TOKEN` post the review and the summary. With `GITHUB_TOKEN` the comments are written as `github-actions[bot]`, an identity shared by every workflow of the repository: ainspector only reads its review state from the summary comment of each mode, but use a dedicated GitHub App if other workflows comment on the same pull requests.

### GitLab CI

ainspector is published as a Docker image, `ghcr.io/iq2i/ainspector` (a tag per release, and `latest`), that you can mirror in the registry of your instance. Pin a release tag (replace `<release>` below with the one you use): the job holds a token that can write to the project, and `latest` would run whatever is published next. Add to your `.gitlab-ci.yml`:

```yaml
ai-review:
  stage: test
  image: ghcr.io/iq2i/ainspector:<release>
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
  allow_failure: true
  script:
    - ainspector review --mode quality,security
  variables:
    GITLAB_TOKEN: $AINSPECTOR_GITLAB_TOKEN
    ANTHROPIC_API_KEY: $ANTHROPIC_API_KEY
    AINSPECTOR_MAX_TOKENS_PER_RUN: "300000"
  artifacts:
    when: always
    expire_in: 30 days
    paths:
      - ainspector-report.json
    reports:
      codequality: gl-code-quality-report.json

# The Code Quality widget compares the merge request with the report of its target branch
ai-review-baseline:
  stage: test
  image: ghcr.io/iq2i/ainspector:<release>
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
  script:
    - echo '[]' > gl-code-quality-report.json
  artifacts:
    reports:
      codequality: gl-code-quality-report.json
```

The `codequality` report makes the findings appear in the Code Quality section of the merge request (available in the Free edition). See [Reports](#reports). Without a report on the target branch, GitLab has nothing to compare with: the second job publishes an empty one.

The job must not be `allow_failure: false`: ainspector never fails a pipeline because of what it found, but it does when it cannot post its comments or write its reports.

#### The GitLab token

`GITLAB_TOKEN` is required. Create a **project access token** dedicated to ainspector (Settings, Access tokens) with the `api` scope and the **Reporter** role, and expose it to the job as a masked CI/CD variable. A protected variable is only given to pipelines of protected branches and tags, which merge request pipelines of other branches are not: with it the job fails for want of a token. Anyone who can edit `.gitlab-ci.yml` in a merge request can print the token, which is why it must be dedicated and limited to the Reporter role. GitLab has no narrower scope that can write merge request notes, and `CI_JOB_TOKEN` cannot: ainspector does not accept it. The comments are posted under the name of the token, which is also how ainspector recognizes its own comments among the others. The token needs nothing else than reading the merge request and its files, and writing notes.

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

Each run stops sending requests once `llm.max_tokens_per_run` tokens were consumed (400000 by default; tokens read from the cache do not count). `AINSPECTOR_MAX_TOKENS_PER_RUN` replaces that default with the cap of your choice, and `ainspector.yaml` can only lower the cap, never raise it: the file comes from the merge request under review, so its author could otherwise raise their own budget. Set it as a CI/CD variable of the project or the group, which a merge request cannot override. The functions left unreviewed are listed in the summary and reviewed by the next run. The summary also gives the tokens used and an estimated cost, for the models whose price is known (`claude-sonnet-5-5` and `claude-opus-5-5`, others through `llm.prices`).

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

# Reports written at the end of a run, relative to the directory of the job
publish:
  code_quality: gl-code-quality-report.json   # "" disables it
  artifact: ainspector-report.json            # "" disables it

# Custom review rules enforced by the AI (same as modes.quality.rules)
rules:
  - "All exceptions must be logged before rethrowing"
  - "Database queries must use prepared statements"
  - "No console.log allowed in production code"
  - "All public functions must have JSDoc comments"
  - "API responses must include proper error codes"
```

### Configuration Options

**ignore.paths** - Glob patterns for files to skip during review, such as test files. Files under `vendor/` or `node_modules/` (at any depth), lock files, minified assets and source maps are never reviewed, whatever the list says. The summary lists the files that sit below a `vendor/` or `node_modules/` directory that is not at the root, since a change could put code there to keep it out of review; if they are all there is, a notice comment says so.

**ignore.authors**, **ignore.title_patterns**, **ignore.labels** - Skip the whole merge request when its author or one of its labels matches (case-insensitive), or when its title matches one of the regular expressions. Handy for dependency bots.

**modes.quality** - `enabled` (default `true`), `model` (default: the model of the LLM backend) and `rules`, which are added after the top-level `rules`.

**modes.security** - Disabled unless `enabled: true`. `scope` is the code given with each unit: `function`, `function+class` (adds the header of the class) or `function+class+related` (the default, adds the related files). `max_findings` caps the findings commented per run (default `5`, at most `50`). `hunks` tells which files without functions are reviewed, and how many lines of context surround each change (defaults above, at most 50 lines each way); lock files (`*.lock`, `package-lock.json`, `pnpm-lock.yaml`) and minified assets are never reviewed that way. `false_positive.model` is the model of the second pass (`claude-opus-5-5` on the Anthropic backend, the default model of the backend otherwise), `false_positive.instructions` is appended to the built-in instructions. `model` is the model of the review itself. As `ainspector.yaml` comes from the merge request, its author can name a more expensive model: the token cap bounds the tokens, not their price.

**publish** - `code_quality` and `artifact` are the paths of the two reports (see [Reports](#reports)), relative to the directory of the job: the defaults are above, an empty string disables the report. An absolute path, a path with `..` or a `.git` folder, or the same file for both reports is refused.

**context.include** - Files to include when generating project context. The AI will read these files to better understand your project's architecture, conventions, and requirements. By default, ainspector automatically searches for common documentation files like `CLAUDE.md`, `AGENTS.md`, `.cursorrules`, `README.md`, and `.github/copilot-instructions.md`.

**context.exclude** - Files to exclude from project context (overrides include patterns). Patterns that are absolute or contain `..` are refused, linked directories are not followed, at most 200 files are read, and files that hold secrets (`.env*`, keys, `.npmrc`, `.netrc`, `.git-credentials`, `.pypirc`, `.docker/config.json`, `.git/`) are never given.

**context.max_bytes** - Size cap of the project context (default `32768`, at most `262144`). Files are added in order until the cap is reached: the one that crosses it is truncated, the following ones are left out, and the summary says so.

**llm** - `provider` (`anthropic` or `openai`), `max_tokens_per_run` and `prices`, see [LLM Configuration](#llm-configuration).

**rules** - Custom project-specific review rules. These rules are enforced by the AI reviewer and any violations will be explicitly reported in the code review comments.

`ainspector.yaml` is read from the checkout of the merge request, so its author can change the settings of the review that judges their own change (disable a mode, add instructions to the false positive filter). When a merge request modifies the file, ainspector says so in the log and in the summary. If the run then stops before reviewing anything (the author is ignored, no mode is enabled, `ignore.paths` leaves nothing to review), it posts a notice comment instead, so that a change cannot silence its own review without a trace.

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

### Reports

Besides the comments, every run writes two files in the directory of the job, even when it found nothing, unless the merge request is ignored:

- `gl-code-quality-report.json`, in the [GitLab Code Quality](https://docs.gitlab.com/ci/testing/code_quality/) format. Declare it under `artifacts.reports.codequality` (see the GitLab CI example) and the findings show in the merge request. Both modes feed it; `security` findings are in the `Security` category, and their severities map to GitLab's (`critical` to `blocker`, `high` to `critical`, `medium` to `major`, `low` to `minor`, `info` to `info`); `quality` findings are `minor`. GitLab only displays the report of the last pipeline, and a run does not review the units it already reviewed: the report therefore also repeats the findings of earlier runs that still hold (their unit did not change), and the findings left out by the cap (but not those the false positive filter never judged, which only reach the artifact). The lines of a repeated finding follow its function when it moves. A finding disappears from it when its unit is reviewed again and no longer reports it.
- `ainspector-report.json`, the complete record of the run for archiving: the units with their status, the findings (commented, left out by the cap, repeated, carried over), the findings cut before the false positive filter, the verdicts of the filter, the tokens and the estimated cost. It holds no source code (the code suggested by a finding is left out), nor the content of the related files.

A report is written even with `--dry-run`, since it is a local file. If a report cannot be written, the job fails after the comments were posted.

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

  A unit gets at most `related.max_files` files (at most `20`) and `related.max_bytes` bytes (at most `200000`): the rest is left out or truncated, and the summary says so. Nothing is joined when no rule finds anything. `vendor/`, `node_modules/`, `var/`, files that hold secrets (`.env*`, keys, `.npmrc`, `.netrc`...) and the paths of `ignore.paths` are never read. Credential-like settings (`password`, `secret`, `key`, `token`) are removed from the `security.yaml` given to the AI. The files come from the checkout of the job: when it is not the head of the merge request (merge result pipeline), the run logs the difference. Changing a related file reviews the units that use it again.

To keep the noise low, findings of `info` severity or under 50% confidence are dropped, findings already reported by a previous run are not repeated (even if their line moved), and a second call to a stronger model discards the false positives. That filter never discards denial of service or resource exhaustion findings. If the filter fails, the findings are published unfiltered and the summary says so. Only the `max_findings` most severe findings are commented, the others are listed in the summary. When nothing is found, no inline comment is posted: only the summary.

### Measuring Usefulness

Ask reviewers to react to each comment of ainspector with a thumbs up (useful) or a thumbs down (not useful). On every following run, ainspector reads the reactions to the comments it posted earlier on the merge request (the 100 most recent), and records them: the summary of a mode tells how many were useful, not useful, or without a verdict, and `ainspector-report.json` lists them per comment (`feedback`: file, line, fingerprint, thumbs). Archive the artifacts of a pilot to compute the rate of useful findings across merge requests, and correct the prompts from it. Reactions are not read with `--force`.

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
| `GITLAB_TOKEN` | Project access token with the `api` scope and the Reporter role (required, `CI_JOB_TOKEN` is not supported) |
| `CI_API_V4_URL` | API URL of the instance (automatic) |
| `CI_PROJECT_PATH` | Project path (automatic) |
| `CI_MERGE_REQUEST_IID` | Merge request ID (automatic) |
| `CI_SERVER_HOST` | GitLab host, used when `CI_API_V4_URL` is not set |

## License

MIT
