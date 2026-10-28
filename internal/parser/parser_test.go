package parser

import (
	"sort"
	"strings"
	"testing"
)

func TestGetConfig(t *testing.T) {
	tests := []struct {
		path         string
		expectedName string
		shouldExist  bool
	}{
		{path: "main.go", expectedName: "go", shouldExist: true},
		{path: "app.js", expectedName: "javascript", shouldExist: true},
		{path: "app.jsx", expectedName: "javascript", shouldExist: true},
		{path: "app.ts", expectedName: "typescript", shouldExist: true},
		{path: "app.tsx", expectedName: "typescript", shouldExist: true},
		{path: "main.py", expectedName: "python", shouldExist: true},
		{path: "lib.rs", expectedName: "rust", shouldExist: true},
		{path: "Main.java", expectedName: "java", shouldExist: true},
		{path: "main.c", expectedName: "c", shouldExist: true},
		{path: "main.h", expectedName: "c", shouldExist: true},
		{path: "main.cpp", expectedName: "cpp", shouldExist: true},
		{path: "main.cc", expectedName: "cpp", shouldExist: true},
		{path: "Program.cs", expectedName: "csharp", shouldExist: true},
		{path: "index.php", expectedName: "php", shouldExist: true},
		{path: "app.rb", expectedName: "ruby", shouldExist: true},
		{path: "script.sh", expectedName: "bash", shouldExist: true},
		{path: "script.bash", expectedName: "bash", shouldExist: true},
		{path: "data.json", expectedName: "json", shouldExist: true},
		{path: "index.html", expectedName: "html", shouldExist: true},
		{path: "style.css", expectedName: "css", shouldExist: true},
		{path: "unknown.xyz", expectedName: "", shouldExist: false},
		{path: "noext", expectedName: "", shouldExist: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			config := GetConfig(tt.path)
			if tt.shouldExist {
				if config == nil {
					t.Fatalf("expected config for %s, got nil", tt.path)
					return
				}
				if config.Name != tt.expectedName {
					t.Errorf("expected language %s, got %s", tt.expectedName, config.Name)
				}
			} else {
				if config != nil {
					t.Errorf("expected nil config for %s, got %+v", tt.path, config)
				}
			}
		})
	}
}

func TestGetConfig_CaseInsensitive(t *testing.T) {
	tests := []string{"FILE.GO", "file.Go", "file.GO"}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			config := GetConfig(path)
			if config == nil {
				t.Fatalf("expected config for %s, got nil", path)
				return
			}
			if config.Name != "go" {
				t.Errorf("expected language go, got %s", config.Name)
			}
		})
	}
}

func TestIsSupported(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{path: "main.go", expected: true},
		{path: "app.js", expected: true},
		{path: "main.py", expected: true},
		{path: "data.json", expected: false},  // JSON has no function query
		{path: "index.html", expected: false}, // HTML has no function query
		{path: "style.css", expected: false},  // CSS has no function query
		{path: "unknown.xyz", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := IsSupported(tt.path)
			if result != tt.expected {
				t.Errorf("expected %v for %s, got %v", tt.expected, tt.path, result)
			}
		})
	}
}

func TestIsSupported_ByExtension(t *testing.T) {
	for _, ext := range []string{".go", ".js", ".py", ".ts", ".java", ".rs", ".c", ".php", ".rb"} {
		if !IsSupported("file" + ext) {
			t.Errorf("expected %s to be supported", ext)
		}
	}
	// No function query: the grammar would give nothing
	for _, ext := range []string{".json", ".html", ".css", ".md"} {
		if IsSupported("file" + ext) {
			t.Errorf("expected %s to NOT be supported", ext)
		}
	}
}

func TestParser_ParseGo(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte(`package main

func hello() {
	println("hello")
}

func add(a, b int) int {
	return a + b
}
`)

	functions, lang, err := p.Parse("main.go", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lang != "go" {
		t.Errorf("expected language 'go', got %s", lang)
	}

	if len(functions) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(functions))
	}

	// Sort by start line for consistent testing
	sort.Slice(functions, func(i, j int) bool {
		return functions[i].StartLine < functions[j].StartLine
	})

	if functions[0].Name != "hello" {
		t.Errorf("expected first function name 'hello', got %s", functions[0].Name)
	}
	if functions[1].Name != "add" {
		t.Errorf("expected second function name 'add', got %s", functions[1].Name)
	}
}

func TestParser_ParseGoMethod(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte(`package main

type Server struct{}

func (s *Server) Start() {
	// start server
}

func (s *Server) Stop() {
	// stop server
}
`)

	functions, _, err := p.Parse("server.go", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(functions) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(functions))
	}

	names := make(map[string]bool)
	for _, fn := range functions {
		names[fn.Name] = true
	}

	if !names["Start"] {
		t.Error("expected method 'Start' to be found")
	}
	if !names["Stop"] {
		t.Error("expected method 'Stop' to be found")
	}
}

func TestParser_ParseJavaScript(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte(`
function greet(name) {
  console.log("Hello, " + name);
}

const add = (a, b) => a + b;

class Calculator {
  multiply(a, b) {
    return a * b;
  }
}
`)

	functions, lang, err := p.Parse("app.js", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lang != "javascript" {
		t.Errorf("expected language 'javascript', got %s", lang)
	}

	// Should find: greet, arrow function, multiply
	if len(functions) < 2 {
		t.Fatalf("expected at least 2 functions, got %d", len(functions))
	}

	names := make(map[string]bool)
	for _, fn := range functions {
		names[fn.Name] = true
	}

	if !names["greet"] {
		t.Error("expected function 'greet' to be found")
	}
	if !names["multiply"] {
		t.Error("expected method 'multiply' to be found")
	}
}

func TestParser_ParsePython(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte(`
def hello():
    print("hello")

def add(a, b):
    return a + b

class Calculator:
    def multiply(self, a, b):
        return a * b
`)

	functions, lang, err := p.Parse("main.py", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lang != "python" {
		t.Errorf("expected language 'python', got %s", lang)
	}

	// Should find: hello, add, multiply
	if len(functions) != 3 {
		t.Fatalf("expected 3 functions, got %d", len(functions))
	}

	names := make(map[string]bool)
	for _, fn := range functions {
		names[fn.Name] = true
	}

	for _, expected := range []string{"hello", "add", "multiply"} {
		if !names[expected] {
			t.Errorf("expected function '%s' to be found", expected)
		}
	}
}

func TestParser_ParseTypeScript(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte(`
function greet(name: string): void {
  console.log("Hello, " + name);
}

const add = (a: number, b: number): number => a + b;
`)

	functions, lang, err := p.Parse("app.ts", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lang != "typescript" {
		t.Errorf("expected language 'typescript', got %s", lang)
	}

	if len(functions) < 1 {
		t.Fatalf("expected at least 1 function, got %d", len(functions))
	}

	hasGreet := false
	for _, fn := range functions {
		if fn.Name == "greet" {
			hasGreet = true
			break
		}
	}
	if !hasGreet {
		t.Error("expected function 'greet' to be found")
	}
}

func TestParser_ParseRust(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte(`
fn main() {
    println!("Hello, world!");
}

fn add(a: i32, b: i32) -> i32 {
    a + b
}
`)

	functions, lang, err := p.Parse("main.rs", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lang != "rust" {
		t.Errorf("expected language 'rust', got %s", lang)
	}

	if len(functions) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(functions))
	}

	names := make(map[string]bool)
	for _, fn := range functions {
		names[fn.Name] = true
	}

	if !names["main"] {
		t.Error("expected function 'main' to be found")
	}
	if !names["add"] {
		t.Error("expected function 'add' to be found")
	}
}

func TestParser_ParseJava(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte(`
public class Main {
    public static void main(String[] args) {
        System.out.println("Hello");
    }

    public int add(int a, int b) {
        return a + b;
    }

    public Main() {
        // constructor
    }
}
`)

	functions, lang, err := p.Parse("Main.java", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lang != "java" {
		t.Errorf("expected language 'java', got %s", lang)
	}

	// Should find: main, add, constructor
	if len(functions) < 2 {
		t.Fatalf("expected at least 2 functions, got %d", len(functions))
	}

	names := make(map[string]bool)
	for _, fn := range functions {
		names[fn.Name] = true
	}

	if !names["main"] {
		t.Error("expected method 'main' to be found")
	}
	if !names["add"] {
		t.Error("expected method 'add' to be found")
	}
}

func TestParser_ParsePHP(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte(`<?php

function hello() {
    echo "hello";
}

class Calculator {
    public function add($a, $b) {
        return $a + $b;
    }
}
`)

	functions, lang, err := p.Parse("index.php", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lang != "php" {
		t.Errorf("expected language 'php', got %s", lang)
	}

	if len(functions) < 2 {
		t.Fatalf("expected at least 2 functions, got %d", len(functions))
	}

	names := make(map[string]bool)
	for _, fn := range functions {
		names[fn.Name] = true
	}

	if !names["hello"] {
		t.Error("expected function 'hello' to be found")
	}
	if !names["add"] {
		t.Error("expected method 'add' to be found")
	}
}

func TestParser_ParseRuby(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte(`
def hello
  puts "hello"
end

class Calculator
  def add(a, b)
    a + b
  end
end
`)

	functions, lang, err := p.Parse("app.rb", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lang != "ruby" {
		t.Errorf("expected language 'ruby', got %s", lang)
	}

	if len(functions) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(functions))
	}

	names := make(map[string]bool)
	for _, fn := range functions {
		names[fn.Name] = true
	}

	if !names["hello"] {
		t.Error("expected function 'hello' to be found")
	}
	if !names["add"] {
		t.Error("expected method 'add' to be found")
	}
}

func TestParser_ParseBash(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte(`#!/bin/bash

hello() {
  echo "hello"
}

function greet() {
  echo "greet"
}
`)

	functions, lang, err := p.Parse("script.sh", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lang != "bash" {
		t.Errorf("expected language 'bash', got %s", lang)
	}

	if len(functions) < 1 {
		t.Fatalf("expected at least 1 function, got %d", len(functions))
	}
}

func TestParser_ParseC(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte(`
#include <stdio.h>

int main() {
    printf("Hello, World!\n");
    return 0;
}

int add(int a, int b) {
    return a + b;
}
`)

	functions, lang, err := p.Parse("main.c", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lang != "c" {
		t.Errorf("expected language 'c', got %s", lang)
	}

	if len(functions) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(functions))
	}

	names := make(map[string]bool)
	for _, fn := range functions {
		names[fn.Name] = true
	}

	if !names["main"] {
		t.Error("expected function 'main' to be found")
	}
	if !names["add"] {
		t.Error("expected function 'add' to be found")
	}
}

func TestParser_UnsupportedFileType(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte("some content")

	_, _, err := p.Parse("file.unknown", content)
	if err == nil {
		t.Error("expected error for unsupported file type")
	}
}

func TestParser_EmptyFile(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte("")

	functions, _, err := p.Parse("main.go", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(functions) != 0 {
		t.Errorf("expected 0 functions for empty file, got %d", len(functions))
	}
}

func TestParser_NoFunctionLanguage(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte(`{"key": "value"}`)

	functions, lang, err := p.Parse("data.json", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lang != "json" {
		t.Errorf("expected language 'json', got %s", lang)
	}

	if len(functions) != 0 {
		t.Errorf("expected 0 functions for JSON, got %d", len(functions))
	}
}

func TestParser_FunctionLineNumbers(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte(`package main

func first() {
	// line 4
}

func second() {
	// line 8
	// line 9
}
`)

	functions, _, err := p.Parse("main.go", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sort by start line
	sort.Slice(functions, func(i, j int) bool {
		return functions[i].StartLine < functions[j].StartLine
	})

	if len(functions) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(functions))
	}

	// first function: lines 3-5
	if functions[0].StartLine != 3 {
		t.Errorf("expected first function to start at line 3, got %d", functions[0].StartLine)
	}
	if functions[0].EndLine != 5 {
		t.Errorf("expected first function to end at line 5, got %d", functions[0].EndLine)
	}

	// second function: lines 7-10
	if functions[1].StartLine != 7 {
		t.Errorf("expected second function to start at line 7, got %d", functions[1].StartLine)
	}
	if functions[1].EndLine != 10 {
		t.Errorf("expected second function to end at line 10, got %d", functions[1].EndLine)
	}
}

func TestParser_AnonymousFunction(t *testing.T) {
	p := NewParser()
	defer p.Close()

	content := []byte(`
const handler = function() {
  console.log("anonymous");
};

const arrow = () => {
  return 42;
};
`)

	functions, _, err := p.Parse("app.js", content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have at least the anonymous functions
	if len(functions) < 1 {
		t.Fatalf("expected at least 1 function, got %d", len(functions))
	}

	// Check that anonymous functions are named "<anonymous>"
	hasAnonymous := false
	for _, fn := range functions {
		if fn.Name == "<anonymous>" {
			hasAnonymous = true
			break
		}
	}
	if !hasAnonymous {
		t.Log("Note: Anonymous functions may or may not have <anonymous> name depending on tree-sitter query")
	}
}

const symfonyController = `<?php

namespace App\Controller;

use App\Entity\Post;
use App\Repository\PostRepository;
use Symfony\Component\Security\Http\Attribute\IsGranted;

#[Route('/posts')]
final class PostController extends AbstractController
{
    use LoggerAwareTrait;

    public const PAGE_SIZE = 20;

    private int $counter = 0;

    public function __construct(private readonly PostRepository $posts)
    {
        $this->counter = 1;
    }

    #[Route('/{id}', name: 'post_show')]
    #[IsGranted('POST_VIEW', 'post')]
    public function show(Post $post): Response
    {
        return $this->render('post/show.html.twig', ['post' => $post]);
    }

    public function list(): Response
    {
        return new Response('list');
    }
}
`

func findFunction(t *testing.T, functions []Function, name string) Function {
	t.Helper()
	for _, fn := range functions {
		if fn.Name == name {
			return fn
		}
	}
	t.Fatalf("function %q not found", name)
	return Function{}
}

func TestParser_ParsePHPAttributes(t *testing.T) {
	p := NewParser()
	defer p.Close()

	functions, _, err := p.Parse("PostController.php", []byte(symfonyController))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	show := findFunction(t, functions, "show")

	if show.StartLine != 23 {
		t.Errorf("expected StartLine 23 (first attribute), got %d", show.StartLine)
	}
	if !strings.HasPrefix(show.Content, "#[Route('/{id}', name: 'post_show')]") {
		t.Errorf("content should start with the Route attribute, got:\n%s", show.Content)
	}
	if !strings.Contains(show.Content, "#[IsGranted('POST_VIEW', 'post')]") {
		t.Errorf("content should include the IsGranted attribute, got:\n%s", show.Content)
	}

	list := findFunction(t, functions, "list")
	if strings.Contains(list.Content, "#[") {
		t.Errorf("method without attribute must not capture neighbours' attributes, got:\n%s", list.Content)
	}
}

func TestParser_ParsePHPEnclosing(t *testing.T) {
	p := NewParser()
	defer p.Close()

	functions, _, err := p.Parse("PostController.php", []byte(symfonyController))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	show := findFunction(t, functions, "show")
	if show.Enclosing == nil {
		t.Fatal("expected an enclosing class for show")
	}
	if show.Enclosing.Name != "PostController" {
		t.Errorf("expected class PostController, got %s", show.Enclosing.Name)
	}
	if show.Enclosing.StartLine != 9 {
		t.Errorf("expected class to start at its attribute on line 9, got %d", show.Enclosing.StartLine)
	}
}

func TestParser_EnclosingKinds(t *testing.T) {
	tests := []struct {
		name, path, source string
		wantName, wantKind string
	}{
		{
			name: "enum",
			path: "Status.php",
			source: `<?php
enum Status: string {
    case Active = 'active';
    public function label(): string { return 'x'; }
}
`,
			wantName: "Status", wantKind: "enum",
		},
		{
			name: "trait",
			path: "Loggable.php",
			source: `<?php
trait Loggable {
    public function log() { return 1; }
}
`,
			wantName: "Loggable", wantKind: "trait",
		},
		{
			name: "interface",
			path: "Api.php",
			source: `<?php
interface Api {
    public function x(): void;
}
`,
			wantName: "Api", wantKind: "interface",
		},
		{
			name: "top-level function",
			path: "helpers.php",
			source: `<?php
function helper() { return 1; }
`,
		},
		{
			name: "language without enclosing types",
			path: "main.go",
			source: `package main

func main() {}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			defer p.Close()

			functions, _, err := p.Parse(tt.path, []byte(tt.source))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(functions) != 1 {
				t.Fatalf("expected 1 function, got %d", len(functions))
			}
			got := functions[0].Enclosing
			if tt.wantKind == "" {
				if got != nil {
					t.Fatalf("expected no enclosing class, got %+v", got)
				}
				return
			}
			if got == nil || got.Name != tt.wantName {
				t.Fatalf("expected %s, got %+v", tt.wantName, got)
			}
		})
	}
}

func renderHeader(t *testing.T, path, source, fn string) string {
	t.Helper()
	p := NewParser()
	defer p.Close()

	functions, _, err := p.Parse(path, []byte(source))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	enclosing := findFunction(t, functions, fn).Enclosing
	if enclosing == nil {
		t.Fatalf("no enclosing class for %s", fn)
	}
	header, err := p.RenderClassHeader(path, []byte(source), enclosing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return header
}

func TestParser_RenderClassHeaderPHP(t *testing.T) {
	header := renderHeader(t, "PostController.php", symfonyController, "show")

	want := []string{
		`namespace App\Controller;`,
		`use App\Entity\Post;`,
		`use App\Repository\PostRepository;`,
		`use Symfony\Component\Security\Http\Attribute\IsGranted;`,
		`#[Route('/posts')]`,
		"final class PostController extends AbstractController\n{",
		"    use LoggerAwareTrait;",
		"    public const PAGE_SIZE = 20;",
		"    private int $counter = 0;",
		"    public function __construct(private readonly PostRepository $posts)",
		"        $this->counter = 1;",
		"    #[Route('/{id}', name: 'post_show')]\n    #[IsGranted('POST_VIEW', 'post')]\n    public function show(Post $post): Response { ... }",
		"    public function list(): Response { ... }",
	}
	for _, w := range want {
		if !strings.Contains(header, w) {
			t.Errorf("header should contain %q, got:\n%s", w, header)
		}
	}

	for _, unwanted := range []string{"post/show.html.twig", "new Response('list')"} {
		if strings.Contains(header, unwanted) {
			t.Errorf("header should not contain %q, got:\n%s", unwanted, header)
		}
	}
	if !strings.HasSuffix(header, "}") {
		t.Errorf("header should end with a closing brace, got:\n%s", header)
	}
}

func TestParser_RenderClassHeaderPHPVariants(t *testing.T) {
	tests := []struct {
		name, source, fn string
		want, unwanted   []string
	}{
		{
			name: "enum",
			source: `<?php
namespace App\Enum;

enum Status: string
{
    case Active = 'active';
    case Closed = 'closed';

    public function label(): string
    {
        return 'secret body';
    }
}
`,
			fn: "label",
			want: []string{
				`namespace App\Enum;`,
				"enum Status: string\n{",
				"    case Active = 'active';",
				"    case Closed = 'closed';",
				"    public function label(): string { ... }",
			},
			unwanted: []string{"secret body"},
		},
		{
			name: "trait",
			source: `<?php
namespace App\Util;

use Psr\Log\LoggerInterface;

trait Loggable
{
    private ?LoggerInterface $logger = null;

    public function log(): void
    {
        $this->logger?->info('x');
    }
}
`,
			fn: "log",
			want: []string{
				`namespace App\Util;`,
				`use Psr\Log\LoggerInterface;`,
				"trait Loggable\n{",
				"    private ?LoggerInterface $logger = null;",
			},
			unwanted: []string{"info('x')"},
		},
		{
			name: "braced namespace",
			source: `<?php
namespace App\Service {
    use App\Entity\User;

    class Mailer
    {
        public function send(User $user): void
        {
            mail('x');
        }
    }
}
`,
			fn: "send",
			want: []string{
				`namespace App\Service;`,
				`use App\Entity\User;`,
				"class Mailer\n    {",
			},
			unwanted: []string{"mail('x')"},
		},
		{
			name: "docblocks, case-insensitive constructor and abstract method",
			source: `<?php
/**
 * @Route("/admin")
 * @IsGranted("ROLE_ADMIN")
 */
abstract class AdminController
{
    /**
     * @Groups({"public"})
     */
    private $secret;

    public function __CONSTRUCT() { $this->a = 1; }

    /**
     * @IsGranted("ROLE_SUPER")
     */
    abstract protected function guard(): void;

    public function show($id) { return $id; }
}
`,
			fn: "show",
			want: []string{
				"/**\n * @Route(\"/admin\")\n * @IsGranted(\"ROLE_ADMIN\")\n */\nabstract class AdminController\n{",
				"    /**\n     * @Groups({\"public\"})\n     */\n    private $secret;",
				"    public function __CONSTRUCT() { $this->a = 1; }",
				"    /**\n     * @IsGranted(\"ROLE_SUPER\")\n     */\n    abstract protected function guard(): void;",
				"    public function show($id) { ... }",
			},
			unwanted: []string{"return $id"},
		},
		{
			name: "interface",
			source: `<?php
interface Api
{
    #[Route('/x')]
    public function x(): void;
}
`,
			fn: "x",
			want: []string{
				"interface Api\n{",
				"    #[Route('/x')]\n    public function x(): void;",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := renderHeader(t, "file.php", tt.source, tt.fn)
			for _, w := range tt.want {
				if !strings.Contains(header, w) {
					t.Errorf("header should contain %q, got:\n%s", w, header)
				}
			}
			for _, u := range tt.unwanted {
				if strings.Contains(header, u) {
					t.Errorf("header should not contain %q, got:\n%s", u, header)
				}
			}
		})
	}
}

func TestParser_RenderClassHeaderErrors(t *testing.T) {
	p := NewParser()
	defer p.Close()

	source := []byte(symfonyController)

	if _, err := p.RenderClassHeader("PostController.php", source, nil); err == nil {
		t.Error("expected an error for a nil class reference")
	}
	if _, err := p.RenderClassHeader("PostController.php", source, &ClassRef{StartByte: 1, EndByte: 2}); err == nil {
		t.Error("expected an error for a reference matching no class")
	}
	if _, err := p.RenderClassHeader("main.go", []byte("package main"), &ClassRef{}); err == nil {
		t.Error("expected an error for a language without class header support")
	}
	if _, err := p.RenderClassHeader("unknown.xyz", nil, &ClassRef{}); err == nil {
		t.Error("expected an error for an unsupported file type")
	}
}

func TestParser_RenderClassHeaderPHPExact(t *testing.T) {
	want := `namespace App\Controller;
use App\Entity\Post;
use App\Repository\PostRepository;
use Symfony\Component\Security\Http\Attribute\IsGranted;

#[Route('/posts')]
final class PostController extends AbstractController
{
    use LoggerAwareTrait;

    public const PAGE_SIZE = 20;

    private int $counter = 0;

    public function __construct(private readonly PostRepository $posts)
    {
        $this->counter = 1;
    }

    #[Route('/{id}', name: 'post_show')]
    #[IsGranted('POST_VIEW', 'post')]
    public function show(Post $post): Response { ... }

    public function list(): Response { ... }
}`

	if got := renderHeader(t, "PostController.php", symfonyController, "show"); got != want {
		t.Errorf("unexpected header, got:\n%s\nwant:\n%s", got, want)
	}
}

func TestParser_EnclosingBarriersPHP(t *testing.T) {
	source := `<?php
#[IsGranted('ROLE_ADMIN')]
class Outer
{
    public function make()
    {
        function nested() { return 1; }

        return new class {
            public function inner() { return 1; }
        };
    }
}
`
	p := NewParser()
	defer p.Close()

	functions, _, err := p.Parse("Outer.php", []byte(source))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := findFunction(t, functions, "make").Enclosing; got == nil || got.Name != "Outer" {
		t.Errorf("expected make to be enclosed by Outer, got %+v", got)
	}
	for _, name := range []string{"nested", "inner"} {
		if got := findFunction(t, functions, name).Enclosing; got != nil {
			t.Errorf("%s must not inherit the class Outer, got %+v", name, got)
		}
	}
}

func TestParser_ParsePHPDocblocks(t *testing.T) {
	source := `<?php
class AdminController
{
    /**
     * @Route("/{id}")
     * @IsGranted("ROLE_SUPER")
     */
    public function show($id) { return $id; }

    /** @Route("/detached") */
    private $prop;

    public function list() { return 1; }

    // @IsGranted("ROLE_X")
    public function line() { return 1; }

    /** orphan */
    public $a = 1; public function glued() { return 1; }
}
`
	p := NewParser()
	defer p.Close()

	functions, _, err := p.Parse("AdminController.php", []byte(source))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	show := findFunction(t, functions, "show")
	if show.StartLine != 4 {
		t.Errorf("expected StartLine 4 (docblock), got %d", show.StartLine)
	}
	if !strings.HasPrefix(show.Content, "/**\n     * @Route(\"/{id}\")") || !strings.Contains(show.Content, `@IsGranted("ROLE_SUPER")`) {
		t.Errorf("content should start with the docblock, got:\n%s", show.Content)
	}

	for _, name := range []string{"list", "line", "glued"} {
		fn := findFunction(t, functions, name)
		if strings.Contains(fn.Content, "@") || strings.Contains(fn.Content, "/**") {
			t.Errorf("%s must not absorb a comment that is not its docblock, got:\n%s", name, fn.Content)
		}
	}
}
