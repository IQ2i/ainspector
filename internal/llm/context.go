package llm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iq2i/ainspector/internal/config"
)

// ProjectContext holds the generated project context
type ProjectContext struct {
	Description string // Raw file contents (US.1) OR LLM summary (legacy)
	Languages   []string
	IsRaw       bool // true if Description contains raw files, false if LLM summary
}

// contextFiles maps file patterns to their descriptions
var contextFiles = map[string]string{
	// Go
	"go.mod": "Go module dependencies",
	"go.sum": "Go dependency checksums",

	// JavaScript/TypeScript
	"package.json":  "Node.js dependencies and scripts",
	"tsconfig.json": "TypeScript configuration",

	// Python
	"requirements.txt": "Python dependencies",
	"pyproject.toml":   "Python project configuration",
	"setup.py":         "Python package setup",
	"Pipfile":          "Pipenv dependencies",

	// PHP
	"composer.json": "PHP dependencies",

	// Rust
	"Cargo.toml": "Rust dependencies and project config",

	// Ruby
	"Gemfile": "Ruby dependencies",

	// Java
	"pom.xml":      "Maven dependencies",
	"build.gradle": "Gradle build configuration",

	// .NET
	"*.csproj":        "C# project file",
	"packages.config": ".NET package references",

	// Documentation and AI helpers
	"README.md":                       "Project documentation",
	"CLAUDE.md":                       "Claude AI instructions",
	"AGENTS.md":                       "AI agents instructions",
	".cursorrules":                    "Cursor IDE AI rules",
	".github/copilot-instructions.md": "GitHub Copilot instructions",
}

// GenerateProjectContext analyzes the project and generates a concise context description
func GenerateProjectContext(ctx context.Context, client *Client, projectRoot string, languages []string, contextConfig *config.ContextConfig) (*ProjectContext, error) {
	// If config is provided and has include patterns, use config-based collection
	if contextConfig != nil && len(contextConfig.Include) > 0 {
		return generateConfigBasedContext(projectRoot, languages, contextConfig)
	}

	// Legacy behavior: use hardcoded patterns and LLM summarization
	return generateLegacyContext(ctx, client, projectRoot, languages)
}

// generateConfigBasedContext builds context from user-configured files
func generateConfigBasedContext(projectRoot string, languages []string, contextConfig *config.ContextConfig) (*ProjectContext, error) {
	files, warnings, err := contextConfig.CollectContextFiles(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to collect context files: %w", err)
	}

	// Log warnings
	for _, warning := range warnings {
		fmt.Printf("Warning: %s\n", warning)
	}

	if len(files) == 0 {
		return &ProjectContext{
			Description: "",
			Languages:   languages,
			IsRaw:       true,
		}, nil
	}

	// Build raw context string with file contents
	var sb strings.Builder

	// Sort file paths for deterministic output
	var paths []string
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		content := files[path]
		sb.WriteString(fmt.Sprintf("=== %s ===\n", path))
		sb.WriteString(content)
		sb.WriteString("\n\n")
	}

	return &ProjectContext{
		Description: sb.String(),
		Languages:   languages,
		IsRaw:       true,
	}, nil
}

// generateLegacyContext uses hardcoded patterns and LLM summarization
func generateLegacyContext(ctx context.Context, client *Client, projectRoot string, languages []string) (*ProjectContext, error) {
	// Find relevant files
	files, err := findContextFiles(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to find context files: %w", err)
	}

	if len(files) == 0 {
		// No context files found, return minimal context
		return &ProjectContext{
			Description: "",
			Languages:   languages,
			IsRaw:       false,
		}, nil
	}

	// Build prompt with file contents
	prompt := buildContextPrompt(files, languages)

	// Generate context using LLM
	messages := []ChatMessage{
		{
			Role:    "system",
			Content: contextSystemPrompt,
		},
		{
			Role:    "user",
			Content: prompt,
		},
	}

	description, err := client.Complete(ctx, messages)
	if err != nil {
		// If context generation fails, continue without it
		fmt.Printf("Warning: failed to generate project context: %v\n", err)
		return &ProjectContext{
			Description: "",
			Languages:   languages,
			IsRaw:       false,
		}, nil
	}

	return &ProjectContext{
		Description: strings.TrimSpace(description),
		Languages:   languages,
		IsRaw:       false,
	}, nil
}

const contextSystemPrompt = `You are a technical analyst. You will receive project configuration files and documentation.

Your task is to generate a CONCISE project context (2-4 sentences) that includes:
- Primary language(s) and framework(s)
- Key dependencies or libraries
- Project type (web app, CLI tool, library, etc.)
- Any critical architectural patterns mentioned

Keep it brief and focused on what's most relevant for code review.
Do NOT include version numbers, build configurations, or minor details.
Respond with ONLY the context description, no preamble or explanation.`

// findContextFiles searches for relevant context files in the project
func findContextFiles(projectRoot string) (map[string]string, error) {
	found := make(map[string]string)

	for pattern := range contextFiles {
		// Handle glob patterns
		if strings.Contains(pattern, "*") {
			matches, err := filepath.Glob(filepath.Join(projectRoot, pattern))
			if err != nil {
				continue
			}
			for _, match := range matches {
				content, err := os.ReadFile(match)
				if err != nil {
					continue
				}
				relPath, _ := filepath.Rel(projectRoot, match)
				found[relPath] = string(content)
			}
		} else {
			// Direct file path
			fullPath := filepath.Join(projectRoot, pattern)
			content, err := os.ReadFile(fullPath)
			if err != nil {
				continue // File doesn't exist, skip it
			}
			found[pattern] = string(content)
		}
	}

	return found, nil
}

// buildContextPrompt builds the prompt for context generation
func buildContextPrompt(files map[string]string, languages []string) string {
	var sb strings.Builder

	sb.WriteString("Analyze this project and provide a concise context description.\n\n")

	if len(languages) > 0 {
		sb.WriteString(fmt.Sprintf("Detected languages: %s\n\n", strings.Join(languages, ", ")))
	}

	sb.WriteString("Project files:\n\n")

	// Order: AI instructions first, then README, then config files
	aiFiles := []string{"CLAUDE.md", "AGENTS.md", ".cursorrules", ".github/copilot-instructions.md"}
	readme := "README.md"

	// Process AI instruction files first
	for _, file := range aiFiles {
		if content, ok := files[file]; ok {
			sb.WriteString(fmt.Sprintf("=== %s ===\n", file))
			// Limit content to avoid overwhelming the LLM
			if len(content) > 3000 {
				sb.WriteString(content[:3000] + "\n... (truncated)\n")
			} else {
				sb.WriteString(content + "\n")
			}
			sb.WriteString("\n")
		}
	}

	// Process README
	if content, ok := files[readme]; ok {
		sb.WriteString(fmt.Sprintf("=== %s ===\n", readme))
		if len(content) > 2000 {
			sb.WriteString(content[:2000] + "\n... (truncated)\n")
		} else {
			sb.WriteString(content + "\n")
		}
		sb.WriteString("\n")
	}

	// Process other config files
	for file, content := range files {
		// Skip already processed files
		isProcessed := false
		for _, f := range append(aiFiles, readme) {
			if file == f {
				isProcessed = true
				break
			}
		}
		if isProcessed {
			continue
		}

		sb.WriteString(fmt.Sprintf("=== %s ===\n", file))
		// Limit content for config files
		if len(content) > 1500 {
			sb.WriteString(content[:1500] + "\n... (truncated)\n")
		} else {
			sb.WriteString(content + "\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
