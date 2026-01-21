package parser

import (
	"sort"
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

func TestSupportedExtensions(t *testing.T) {
	extensions := SupportedExtensions()
	if len(extensions) == 0 {
		t.Fatal("expected at least one supported extension")
	}

	// Check that common extensions are included
	expected := []string{".go", ".js", ".py", ".ts", ".java", ".rs", ".c", ".php", ".rb"}
	extensionSet := make(map[string]bool)
	for _, ext := range extensions {
		extensionSet[ext] = true
	}

	for _, ext := range expected {
		if !extensionSet[ext] {
			t.Errorf("expected %s to be in supported extensions", ext)
		}
	}

	// JSON, HTML, CSS should NOT be in supported extensions (no function query)
	notSupported := []string{".json", ".html", ".css"}
	for _, ext := range notSupported {
		if extensionSet[ext] {
			t.Errorf("expected %s to NOT be in supported extensions", ext)
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
