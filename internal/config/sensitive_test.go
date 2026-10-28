package config

import "testing"

func TestIsSensitivePath(t *testing.T) {
	for _, p := range []string{
		".env", ".env.local", "config/.env.prod", "config/secrets/prod/key.php", "deploy/server.pem", "id_rsa.pub",
		"auth.json", "web/.npmrc", ".git-credentials", "home/.netrc", ".pypirc", ".docker/config.json",
		".git/config", "a/.git/hooks/x", ".ENV", "keys/ID_RSA", "deploy/Server.PEM", ".GIT/config", "home/.aws/credentials", "x/.kube/config", "prod.tfvars", "terraform.tfstate", ".htpasswd", "docs/../.env", "./.env",
	} {
		if !IsSensitivePath(p) {
			t.Errorf("%s must be sensitive", p)
		}
	}
	for _, p := range []string{"README.md", "docs/environment.md", "src/Kernel.php", "config/packages/security.yaml", ".github/workflows/test.yml", ".gitignore", "docs/.docker/readme.md"} {
		if IsSensitivePath(p) {
			t.Errorf("%s must not be sensitive", p)
		}
	}
}
