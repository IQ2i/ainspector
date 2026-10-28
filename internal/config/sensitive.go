package config

import (
	"path"
	"strings"
)

// sensitivePrefixes are folders that hold secrets
var sensitivePrefixes = []string{"config/secrets/"}

// sensitiveFiles are the names, or name patterns, of files that hold credentials
var sensitiveFiles = []string{
	"*.pem", "*.key", "*.p12", "*.pfx", "id_rsa*", "id_ed25519*",
	"auth.json", ".npmrc", ".git-credentials", ".netrc", ".pypirc",
}

// IsSensitivePath tells whether a path of the project, slash separated and relative to its root,
// holds secrets that must never reach a prompt: environment files, keys, credentials of package
// managers and the version control folder.
func IsSensitivePath(rel string) bool {
	rel = path.Clean(rel)
	for _, prefix := range sensitivePrefixes {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	if rel == ".docker/config.json" || strings.HasSuffix(rel, "/.docker/config.json") {
		return true
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
