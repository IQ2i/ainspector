package config

import (
	"path"
	"strings"
)

// sensitivePrefixes are folders that hold secrets
var sensitivePrefixes = []string{"config/secrets/"}

// sensitiveTails are files that hold credentials, wherever they sit in the tree
var sensitiveTails = []string{".docker/config.json", ".aws/credentials", ".kube/config"}

// sensitiveFiles are the names, or name patterns, of files that hold credentials
var sensitiveFiles = []string{
	"*.pem", "*.key", "*.p12", "*.pfx", "id_rsa*", "id_ed25519*",
	"auth.json", ".npmrc", ".git-credentials", ".netrc", ".pypirc", ".htpasswd",
	"*.tfvars", "terraform.tfstate", "terraform.tfstate.*",
}

// IsSensitivePath tells whether a path of the project, slash separated and relative to its root,
// holds secrets that must never reach a prompt: environment files, keys, credentials of package
// managers and the version control folder.
func IsSensitivePath(rel string) bool {
	// Checkouts on macOS and Windows are case insensitive
	rel = strings.ToLower(path.Clean(rel))
	for _, prefix := range sensitivePrefixes {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	for _, tail := range sensitiveTails {
		if rel == tail || strings.HasSuffix(rel, "/"+tail) {
			return true
		}
	}
	segments := strings.Split(rel, "/")
	for _, s := range segments[:len(segments)-1] {
		if s == ".git" {
			return true
		}
	}
	base := segments[len(segments)-1]
	if base == ".git" || base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	for _, pattern := range sensitiveFiles {
		if ok, _ := path.Match(pattern, base); ok {
			return true
		}
	}
	return false
}
