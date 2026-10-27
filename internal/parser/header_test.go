package parser

import "testing"

func TestParser_RenderClassHeaderLanguages(t *testing.T) {
	tests := []struct {
		name, path, source, fn, want string
	}{
		{
			name:   "typescript",
			path:   "user.controller.ts",
			source: "import { A } from 'a';\n\n@Controller('x')\nexport class C extends B {\n  private readonly a: string = 'x';\n  static s = 1;\n\n  constructor(private x: X) { super(); }\n\n  @Get()\n  show(id: string): string { return id; }\n\n  other(): void { run(); }\n}\n",
			fn:     "show",
			want:   "import { A } from 'a';\n\n@Controller('x')\nexport class C extends B {\n  private readonly a: string = 'x';\n\n  static s = 1;\n\n  constructor(private x: X) { super(); }\n\n  @Get()\n  show(id: string): string { ... }\n\n  other(): void { ... }\n}",
		},
		{
			name:   "javascript",
			path:   "c.js",
			source: "import a from 'a';\n\nclass C extends B {\n  #p = 1;\n  constructor() { super(); }\n  show(id) { return id; }\n}\n",
			fn:     "show",
			want:   "import a from 'a';\n\nclass C extends B {\n  #p = 1;\n\n  constructor() { super(); }\n\n  show(id) { ... }\n}",
		},
		{
			name:   "python",
			path:   "views.py",
			source: "import os\nfrom a import b\n\n\n@dataclass\nclass C(B):\n    x = 1\n\n    def __init__(self):\n        pass\n\n    @route('/x')\n    def show(self, id):\n        return id\n\n    def other(self):\n        return 1\n",
			fn:     "show",
			want:   "import os\nfrom a import b\n\n@dataclass\nclass C(B):\n    x = 1\n\n    def __init__(self):\n        pass\n\n    @route('/x')\n    def show(self, id): ...\n\n    def other(self): ...",
		},
		{
			name:   "java",
			path:   "C.java",
			source: "package a.b;\n\nimport a.C;\n\n@RestController\n@RequestMapping(\"/x\")\npublic class C extends B {\n  private final String a = \"x\";\n\n  public C(X x) { this.x = x; }\n\n  @GetMapping\n  public String show(int id) { return \"\"; }\n}\n",
			fn:     "show",
			want:   "package a.b;\nimport a.C;\n\n@RestController\n@RequestMapping(\"/x\")\npublic class C extends B {\n  private final String a = \"x\";\n\n  public C(X x) { this.x = x; }\n\n  @GetMapping\n  public String show(int id) { ... }\n}",
		},
		{
			name:   "csharp file scoped namespace",
			path:   "C.cs",
			source: "using A.B;\n\nnamespace N;\n\n[ApiController]\npublic class C : B {\n    public string Show(int id) { return \"\"; }\n}\n",
			fn:     "Show",
			want:   "using A.B;\nnamespace N;\n\n[ApiController]\npublic class C : B {\n    public string Show(int id) { ... }\n}",
		},
		{
			name:   "csharp braced namespace",
			path:   "C.cs",
			source: "using A.B;\n\nnamespace N {\n  [ApiController]\n  public class C : B {\n    public string P { get; set; }\n\n    [HttpGet]\n    public string Show(int id) { return \"\"; }\n  }\n}\n",
			fn:     "Show",
			want:   "using A.B;\nnamespace N;\n\n[ApiController]\n  public class C : B {\n    public string P { get; set; }\n\n    [HttpGet]\n    public string Show(int id) { ... }\n}",
		},
		{
			name:   "ruby",
			path:   "c.rb",
			source: "require 'a'\n\nclass C < B\n  include M\n  before_action :auth\n  X = 1\n\n  def initialize(x)\n    @x = x\n  end\n\n  def show(id)\n    id\n  end\nend\n",
			fn:     "show",
			want:   "require 'a'\n\nclass C < B\n  include M\n\n  before_action :auth\n\n  X = 1\n\n  def initialize(x)\n    @x = x\n  end\n\n  def show(id) ... end\nend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderHeader(t, tt.path, tt.source, tt.fn); got != tt.want {
				t.Errorf("header mismatch\n--- want ---\n%s\n--- got ---\n%s", tt.want, got)
			}
		})
	}
}

func TestParser_FunctionContentIncludesDecorators(t *testing.T) {
	tests := []struct {
		name, path, source, fn string
		wantStart              int
		wantPrefix             string
	}{
		{"python", "v.py", "class C:\n    @route('/x')\n    @login_required\n    def show(self):\n        pass\n", "show", 2, "@route('/x')"},
		{"typescript", "c.ts", "class C {\n  @Get()\n  @Auth()\n  show() {}\n}\n", "show", 2, "@Get()"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			defer p.Close()
			fns, _, err := p.Parse(tt.path, []byte(tt.source))
			if err != nil {
				t.Fatal(err)
			}
			fn := findFunction(t, fns, tt.fn)
			if fn.StartLine != tt.wantStart || len(fn.Content) < len(tt.wantPrefix) || fn.Content[:len(tt.wantPrefix)] != tt.wantPrefix {
				t.Errorf("start %d, content %q", fn.StartLine, fn.Content)
			}
		})
	}
}

func TestParser_EnclosingBarriersOtherLanguages(t *testing.T) {
	tests := []struct {
		name, path, source, fn string
	}{
		{"python nested function", "a.py", "class C:\n    def m(self):\n        def inner():\n            pass\n", "inner"},
		{"javascript nested arrow", "a.js", "class C {\n  m() {\n    const f = () => 1;\n  }\n}\n", "<anonymous>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			defer p.Close()
			fns, _, err := p.Parse(tt.path, []byte(tt.source))
			if err != nil {
				t.Fatal(err)
			}
			if got := findFunction(t, fns, tt.fn).Enclosing; got != nil {
				t.Errorf("expected no enclosing class, got %+v", got)
			}
		})
	}
}

func TestParser_NoClassHeaderForGo(t *testing.T) {
	p := NewParser()
	defer p.Close()
	fns, _, err := p.Parse("a.go", []byte("package a\n\ntype T struct{}\n\nfunc (T) M() {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if fns[0].Enclosing != nil {
		t.Errorf("Go methods have no enclosing class, got %+v", fns[0].Enclosing)
	}
}
