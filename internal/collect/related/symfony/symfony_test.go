package symfony

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/collect/related"
)

func tree(t *testing.T, files map[string]string) related.Repo {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return related.NewRepo(root, nil)
}

// resolve runs every resolver of the preset on a unit and returns the paths found
func resolve(t *testing.T, repo related.Repo, u collect.ReviewUnit) map[string]collect.RelatedFile {
	t.Helper()
	found := map[string]collect.RelatedFile{}
	for _, r := range Resolvers() {
		files, err := r.Resolve(context.Background(), &u, repo)
		if err != nil {
			t.Fatalf("%s: %v", r.Name(), err)
		}
		for _, f := range files {
			found[f.Path] = f
		}
	}
	return found
}

func wantPaths(t *testing.T, got map[string]collect.RelatedFile, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("expected %v, got %v", want, keys(got))
	}
	for _, p := range want {
		if _, ok := got[p]; !ok {
			t.Errorf("missing %s, got %v", p, keys(got))
		}
	}
}

func keys(m map[string]collect.RelatedFile) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

const securityYAML = `security:
    password_hashers:
        App\Entity\User: auto
    providers:
        users:
            entity: {class: App\Entity\User}
    firewalls:
        main:
            lazy: true
    access_control:
        - { path: ^/admin, roles: ROLE_ADMIN }
`

func TestController_JoinsVoterRulesAndTemplate(t *testing.T) {
	repo := tree(t, map[string]string{
		"src/Security/Voter/InvoiceVoter.php": "class InvoiceVoter { const VIEW = 'INVOICE_VIEW'; }",
		"src/Security/Voter/OtherVoter.php":   "class OtherVoter { const X = 'OTHER'; }",
		"config/packages/security.yaml":       securityYAML,
		"templates/invoice/show.html.twig":    "{{ invoice.number }}",
		"templates/invoice/other.html.twig":   "other",
	})
	u := collect.ReviewUnit{
		FilePath: "src/Controller/InvoiceController.php",
		Name:     "show",
		Content: "#[Route('/{id}')]\n#[IsGranted('INVOICE_VIEW', 'invoice')]\npublic function show(#[MapEntity] Invoice $invoice): Response\n{\n" +
			"    return $this->render('invoice/show.html.twig', ['invoice' => $invoice]);\n}",
	}

	got := resolve(t, repo, u)

	wantPaths(t, got, "src/Security/Voter/InvoiceVoter.php", "config/packages/security.yaml", "templates/invoice/show.html.twig")
	rules := got["config/packages/security.yaml"].Content
	if !strings.Contains(rules, "access_control") || !strings.Contains(rules, "firewalls") {
		t.Errorf("the access rules are missing: %s", rules)
	}
	if strings.Contains(rules, "password_hashers") || strings.Contains(rules, "providers") {
		t.Errorf("the rest of the security configuration must be left out: %s", rules)
	}
}

func TestController_VoterFoundByClassLevelAttributeAndConstant(t *testing.T) {
	repo := tree(t, map[string]string{
		"src/Security/AccountVoter.php": "class AccountVoter { const VIEW = 'view'; }",
		"src/Security/Unrelated.php":    "class Unrelated {}",
	})
	u := collect.ReviewUnit{
		FilePath:  "src/Controller/AccountController.php",
		Content:   "public function index(): Response\n{\n}",
		Enclosing: &collect.ClassHeader{Rendered: "#[IsGranted(AccountVoter::VIEW)]\nclass AccountController"},
	}
	wantPaths(t, resolve(t, repo, u), "src/Security/AccountVoter.php")
}

func TestController_VoterReferencedByName(t *testing.T) {
	repo := tree(t, map[string]string{"src/Security/PostVoter.php": "class PostVoter {}"})
	u := collect.ReviewUnit{
		FilePath: "src/Controller/PostController.php",
		Content:  "#[IsGranted(PostVoter::EDIT, 'post')]\npublic function edit(): Response\n{\n}",
	}
	wantPaths(t, resolve(t, repo, u), "src/Security/PostVoter.php")
}

func TestController_VoterNamedAfterTheEntity(t *testing.T) {
	repo := tree(t, map[string]string{
		"src/Security/Voter/AccountVoter.php": "class AccountVoter {}",
		"src/Security/Voter/OtherVoter.php":   "class OtherVoter {}",
	})
	u := collect.ReviewUnit{
		FilePath: "src/Controller/AccountController.php",
		Name:     "show",
		Content:  "#[Route('/{id}')]\npublic function show(Request $request, Account $account): Response\n{\n}",
	}
	wantPaths(t, resolve(t, repo, u), "src/Security/Voter/AccountVoter.php")
}

func TestController_NothingToJoin(t *testing.T) {
	repo := tree(t, map[string]string{"src/Other.php": "x"})
	u := collect.ReviewUnit{FilePath: "src/Controller/A.php", Content: "public function index(): Response\n{\n}"}
	wantPaths(t, resolve(t, repo, u))
}

func TestController_TemplatesCannotEscapeTheTemplatesFolder(t *testing.T) {
	repo := tree(t, map[string]string{"secret.twig": "x", "templates/@bundle/a.twig": "x"})
	u := collect.ReviewUnit{
		FilePath: "src/Controller/A.php",
		Content:  "return $this->render('../secret.twig');\nreturn $this->render('@bundle/a.twig');",
	}
	wantPaths(t, resolve(t, repo, u))
}

func TestController_TemplateAttribute(t *testing.T) {
	repo := tree(t, map[string]string{"templates/home.html.twig": "home"})
	u := collect.ReviewUnit{FilePath: "src/Controller/A.php", Content: "#[Template('home.html.twig')]\npublic function home(): array\n{\n}"}
	wantPaths(t, resolve(t, repo, u), "templates/home.html.twig")
}

func TestController_AccessRulesFailClosed(t *testing.T) {
	for name, content := range map[string]string{
		"invalid yaml":     "security: [unclosed",
		"no security key":  "framework:\n    secret: abc\n",
		"no access rules":  "security:\n    password_hashers:\n        App\\Entity\\User: auto\n",
		"security is list": "security: [a, b]\n",
	} {
		repo := tree(t, map[string]string{"config/packages/security.yaml": content})
		u := collect.ReviewUnit{FilePath: "src/Controller/A.php", Content: "public function a(): Response\n{\n}"}
		if got := resolve(t, repo, u); len(got) != 0 {
			t.Errorf("%s: the file must not be given as it is, got %v", name, keys(got))
		}
	}
}

func TestController_AccessRulesKeepTheOrderAndDropSecrets(t *testing.T) {
	repo := tree(t, map[string]string{"config/packages/security.yaml": `security:
    password_hashers:
        App\Entity\User: auto
    firewalls:
        dev:
            pattern: ^/_profiler
            security: false
        main:
            # the main firewall
            remember_me:
                secret: '%kernel.secret%'
                lifetime: 60
        api:
            stateless: true
            access_token:
                token_handler: app.handler
                api_key: abc123
        ldap:
            http_basic_ldap:
                service: Symfony\Component\Ldap\Ldap
                search_dn: cn=admin
                search_password: ldap-admin-pass
                signing_key: k3y
    access_control:
        - { path: ^/admin, roles: ROLE_ADMIN }
        - { path: ^/, roles: PUBLIC_ACCESS }
`})
	u := collect.ReviewUnit{FilePath: "src/Controller/A.php", Content: "public function a(): Response\n{\n}"}
	rules := resolve(t, repo, u)["config/packages/security.yaml"].Content

	if strings.Index(rules, "dev:") > strings.Index(rules, "main:") || strings.Index(rules, "main:") > strings.Index(rules, "api:") {
		t.Errorf("the firewalls must stay in the order of the file:\n%s", rules)
	}
	if strings.Index(rules, "ROLE_ADMIN") > strings.Index(rules, "PUBLIC_ACCESS") {
		t.Errorf("the access_control rules must stay in order:\n%s", rules)
	}
	for _, leaked := range []string{"ldap-admin-pass", "abc123", "k3y"} {
		if strings.Contains(rules, leaked) {
			t.Errorf("%s must be dropped:\n%s", leaked, rules)
		}
	}
	if !strings.Contains(rules, "token_handler: app.handler") || !strings.Contains(rules, "search_dn") {
		t.Errorf("sections and plain settings must stay:\n%s", rules)
	}
	if strings.Contains(rules, "kernel.secret") || strings.Contains(rules, "password_hashers") || !strings.Contains(rules, "lifetime") {
		t.Errorf("secrets and unrelated sections must go, the rest stay:\n%s", rules)
	}
}

func TestController_DoesNotReadThroughLinks(t *testing.T) {
	root := t.TempDir()
	write := func(name, content string) {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".env.local", "APP_SECRET=hunter2")
	write("templates/placeholder", "")
	if err := os.Symlink("../.env.local", filepath.Join(root, "templates", "x.html.twig")); err != nil {
		t.Skip("symbolic links unavailable")
	}
	u := collect.ReviewUnit{FilePath: "src/Controller/A.php", Content: "public function a(): Response\n{\n    return $this->render('x.html.twig');\n}"}

	wantPaths(t, resolve(t, related.NewRepo(root, nil), u))
}

func TestEntity_NameMustBeAnIdentifier(t *testing.T) {
	repo := tree(t, map[string]string{"src/Entity/Weird[1].php": "class X {}", "src/Repo/Weird[1]Repository.php": "x"})
	wantPaths(t, resolve(t, repo, collect.ReviewUnit{FilePath: "src/Entity/Weird[1].php"}))
}

func TestEntity_JoinsRepositoryAndSerializerConfig(t *testing.T) {
	repo := tree(t, map[string]string{
		"src/Entity/Invoice.php":               "<?php\n#[ORM\\Entity(repositoryClass: InvoiceRepository::class)]\nclass Invoice {}",
		"src/Repository/InvoiceRepository.php": "class InvoiceRepository {}",
		"config/serializer/Invoice.yaml":       "groups: []",
		"config/serializer/Other.yaml":         "other",
	})
	u := collect.ReviewUnit{FilePath: "src/Entity/Invoice.php", Name: "getNumber"}

	wantPaths(t, resolve(t, repo, u), "src/Repository/InvoiceRepository.php", "config/serializer/Invoice.yaml")
}

func TestEntity_GroupsAttributesReplaceTheSerializerConfig(t *testing.T) {
	repo := tree(t, map[string]string{
		"src/Entity/Invoice.php":         "<?php\nclass Invoice { #[Groups(['read'])] private $n; }",
		"config/serializer/Invoice.yaml": "groups: []",
	})
	wantPaths(t, resolve(t, repo, collect.ReviewUnit{FilePath: "src/Entity/Invoice.php"}))
}

func TestForm_JoinsTheMappedEntity(t *testing.T) {
	repo := tree(t, map[string]string{
		"src/Form/InvoiceType.php": "<?php\nuse App\\Entity\\Invoice;\nclass InvoiceType { function configureOptions($r) { $r->setDefaults(['data_class' => Invoice::class]); } }",
		"src/Entity/Invoice.php":   "class Invoice {}",
	})
	wantPaths(t, resolve(t, repo, collect.ReviewUnit{FilePath: "src/Form/InvoiceType.php"}), "src/Entity/Invoice.php")
}

func TestForm_FallsBackToTheClassName(t *testing.T) {
	repo := tree(t, map[string]string{
		"src/Form/InvoiceType.php": "<?php\nclass InvoiceType { 'data_class' => \\Vendor\\Model\\Invoice::class }",
		"src/Domain/Invoice.php":   "class Invoice {}",
	})
	wantPaths(t, resolve(t, repo, collect.ReviewUnit{FilePath: "src/Form/InvoiceType.php"}), "src/Domain/Invoice.php")
}

func TestForm_WithoutDataClass(t *testing.T) {
	repo := tree(t, map[string]string{"src/Form/FreeType.php": "<?php class FreeType {}"})
	wantPaths(t, resolve(t, repo, collect.ReviewUnit{FilePath: "src/Form/FreeType.php"}))
}

func TestVoter_JoinsTheControllersInvokingIt(t *testing.T) {
	repo := tree(t, map[string]string{
		"src/Security/InvoiceVoter.php":          "class InvoiceVoter { const VIEW = 'INVOICE_VIEW'; }",
		"src/Controller/ByAttribute.php":         "#[IsGranted('INVOICE_VIEW', 'invoice')]",
		"src/Controller/ByCall.php":              "$this->denyAccessUnlessGranted('INVOICE_VIEW', $invoice);",
		"src/Controller/ByConstant.php":          "#[IsGranted(InvoiceVoter::VIEW)]",
		"src/Controller/Unrelated.php":           "#[IsGranted('OTHER')]",
		"src/Controller/MentionsOnlyAString.php": "$x = 'INVOICE_VIEW';",
		"src/Service/NotAController.php":         "#[IsGranted('INVOICE_VIEW')]",
	})
	u := collect.ReviewUnit{FilePath: "src/Security/InvoiceVoter.php", Name: "voteOnAttribute"}

	wantPaths(t, resolve(t, repo, u), "src/Controller/ByAttribute.php", "src/Controller/ByCall.php", "src/Controller/ByConstant.php")
}

func TestTemplate_JoinsTheCodeRenderingIt(t *testing.T) {
	repo := tree(t, map[string]string{
		"src/Controller/A.php": "$this->render('invoice/show.html.twig')",
		"src/Controller/B.php": "$this->render('invoice/other.html.twig')",
	})
	u := collect.ReviewUnit{FilePath: "templates/invoice/show.html.twig", Language: "twig"}

	wantPaths(t, resolve(t, repo, u), "src/Controller/A.php")
}

func TestResolversIgnoreOtherFiles(t *testing.T) {
	repo := tree(t, map[string]string{"src/Controller/A.php": "x"})
	wantPaths(t, resolve(t, repo, collect.ReviewUnit{FilePath: "main.go"}))
}

func TestClassPath(t *testing.T) {
	if got := classPath(`App\Entity\Invoice`); got != "src/Entity/Invoice.php" {
		t.Errorf("got %q", got)
	}
	if classPath(`Vendor\Invoice`) != "" {
		t.Error("only the App namespace maps to src/")
	}
}
