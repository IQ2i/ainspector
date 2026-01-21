module github.com/iq2i/ainspector

go 1.24.0

require (
	github.com/google/go-github/v57 v57.0.0
	github.com/sourcegraph/go-diff v0.7.0
	github.com/spf13/cobra v1.10.2
	github.com/tree-sitter/go-tree-sitter v0.25.0
	github.com/tree-sitter/tree-sitter-bash v0.25.1
	github.com/tree-sitter/tree-sitter-c v0.24.1
	github.com/tree-sitter/tree-sitter-c-sharp v0.23.1
	github.com/tree-sitter/tree-sitter-cpp v0.23.4
	github.com/tree-sitter/tree-sitter-css v0.25.0
	github.com/tree-sitter/tree-sitter-go v0.25.0
	github.com/tree-sitter/tree-sitter-html v0.23.2
	github.com/tree-sitter/tree-sitter-java v0.23.5
	github.com/tree-sitter/tree-sitter-javascript v0.25.0
	github.com/tree-sitter/tree-sitter-json v0.24.8
	github.com/tree-sitter/tree-sitter-php v0.24.0
	github.com/tree-sitter/tree-sitter-python v0.23.6
	github.com/tree-sitter/tree-sitter-ruby v0.23.1
	github.com/tree-sitter/tree-sitter-rust v0.23.2
	github.com/tree-sitter/tree-sitter-typescript v0.23.2
	gitlab.com/gitlab-org/api/client-go v1.15.0
	golang.org/x/oauth2 v0.34.0
)

require (
	github.com/bmatcuk/doublestar/v4 v4.9.2 // indirect
	github.com/google/go-querystring v1.2.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.8 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mattn/go-pointer v0.0.1 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/time v0.14.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// Replace directives to fix module path mismatches in tree-sitter packages
replace (
	github.com/tree-sitter/tree-sitter-c-sharp/bindings/go => github.com/tree-sitter/tree-sitter-c-sharp v0.23.1
	github.com/tree-sitter/tree-sitter-java/bindings/go => github.com/tree-sitter/tree-sitter-java v0.23.5
	github.com/tree-sitter/tree-sitter-php/bindings/go => github.com/tree-sitter/tree-sitter-php v0.24.0
	github.com/tree-sitter/tree-sitter-python/bindings/go => github.com/tree-sitter/tree-sitter-python v0.23.6
	github.com/tree-sitter/tree-sitter-rust/bindings/go => github.com/tree-sitter/tree-sitter-rust v0.23.2
	github.com/tree-sitter/tree-sitter-typescript/bindings/go => github.com/tree-sitter/tree-sitter-typescript v0.23.2
)
