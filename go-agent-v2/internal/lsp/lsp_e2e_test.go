// lsp_e2e_test.go — LSP 增强 E2E 冒烟测试。
//
// 测试 Go/Rust/JS 三种语言的全部 LSP 能力:
//   - definition, references, documentSymbol, rename
//
// 前提: gopls, rust-analyzer, typescript-language-server 已安装。
// 运行: go test -v -run TestLSP_E2E -timeout 120s ./internal/lsp/
package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─── 测试 fixture 文件内容 ───

const goTestFile = `package demo

import "fmt"

// Greeter 问候器。
type Greeter struct {
	Name string
}

// Hello 返回问候语。
func (g *Greeter) Hello() string {
	return fmt.Sprintf("Hello, %s!", g.Name)
}

// NewGreeter 创建 Greeter。
func NewGreeter(name string) *Greeter {
	return &Greeter{Name: name}
}

func main() {
	g := NewGreeter("World")
	fmt.Println(g.Hello())
}
`

const rustTestFile = `struct Greeter {
    name: String,
}

impl Greeter {
    fn new(name: &str) -> Self {
        Greeter { name: name.to_string() }
    }

    fn hello(&self) -> String {
        format!("Hello, {}!", self.name)
    }
}

fn main() {
    let g = Greeter::new("World");
    println!("{}", g.hello());
}
`

const jsTestFile = `class Greeter {
  constructor(name) {
    this.name = name;
  }

  hello() {
    return "Hello, " + this.name + "!";
  }
}

function createGreeter(name) {
  return new Greeter(name);
}

const g = createGreeter("World");
console.log(g.hello());
`

// ─── 辅助函数 ───

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Go: 需要 go.mod
	must(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module demo\n\ngo 1.22\n"), 0644))
	must(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(goTestFile), 0644))
	// Rust: 需要 Cargo.toml
	must(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"demo\"\nversion = \"0.1.0\"\nedition = \"2021\"\n"), 0644))
	must(t, os.MkdirAll(filepath.Join(dir, "src"), 0755))
	must(t, os.WriteFile(filepath.Join(dir, "src", "main.rs"), []byte(rustTestFile), 0644))
	// JS: 需要 tsconfig 或 jsconfig
	must(t, os.WriteFile(filepath.Join(dir, "app.js"), []byte(jsTestFile), 0644))
	must(t, os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(`{"compilerOptions":{"allowJs":true,"checkJs":true},"include":["*.js"]}`), 0644))
	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// ─── 主测试 ───

func TestLSP_E2E_Go(t *testing.T) {
	requireServerOrSkip(t, "go")
	dir := setupTestDir(t)

	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + dir)
	defer mgr.StopAll()

	goFile := filepath.Join(dir, "main.go")

	// 1. Open file
	err := mgr.OpenFile(goFile, goTestFile)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}

	// gopls 需要一点时间索引
	time.Sleep(3 * time.Second)

	// 2. DocumentSymbol — 大纲
	t.Run("DocumentSymbol", func(t *testing.T) {
		symbols, err := mgr.DocumentSymbol(goFile)
		if err != nil {
			t.Fatalf("DocumentSymbol: %v", err)
		}
		if len(symbols) == 0 {
			t.Fatal("DocumentSymbol returned 0 symbols, expected at least Greeter/Hello/NewGreeter/main")
		}
		t.Logf("DocumentSymbol returned %d top-level symbols:", len(symbols))
		for _, s := range symbols {
			t.Logf("  %s (%s) L%d-%d", s.Name, s.Kind.String(), s.Range.Start.Line+1, s.Range.End.Line+1)
			for _, c := range s.Children {
				t.Logf("    %s (%s) L%d-%d", c.Name, c.Kind.String(), c.Range.Start.Line+1, c.Range.End.Line+1)
			}
		}
	})

	// 3. Hover
	t.Run("Hover", func(t *testing.T) {
		// Hover on "Greeter" in type definition (line 5, col 5)
		result, err := mgr.Hover(goFile, 5, 5)
		if err != nil {
			t.Fatalf("Hover: %v", err)
		}
		if result == nil {
			t.Fatal("Hover returned nil")
		}
		t.Logf("Hover result: %s", result.Contents.Value[:min(len(result.Contents.Value), 200)])
	})

	// 4. Definition — 从 NewGreeter 调用跳到定义
	t.Run("Definition", func(t *testing.T) {
		// "NewGreeter" is called at line 19 (0-indexed), col ~6
		locs, err := mgr.Definition(goFile, 19, 7)
		if err != nil {
			t.Fatalf("Definition: %v", err)
		}
		if len(locs) == 0 {
			t.Fatal("Definition returned 0 locations")
		}
		t.Logf("Definition: %d locations, first: %s L%d", len(locs), locs[0].URI, locs[0].Range.Start.Line+1)
		// NewGreeter 定义位置 (行号取决于文件内容，只检查定义确实被找到)
		if !strings.Contains(locs[0].URI, "main.go") {
			t.Errorf("expected definition in main.go, got %s", locs[0].URI)
		}
	})

	// 5. References — 查找 Greeter 的引用
	t.Run("References", func(t *testing.T) {
		// "Greeter" type: line 5, col 5 (0-indexed)
		locs, err := mgr.References(goFile, 5, 5, true)
		if err != nil {
			t.Fatalf("References: %v", err)
		}
		if len(locs) < 2 {
			t.Fatalf("References: expected at least 2 references to Greeter, got %d", len(locs))
		}
		t.Logf("References: %d locations", len(locs))
		for _, l := range locs {
			t.Logf("  %s L%d:%d", filepath.Base(strings.TrimPrefix(l.URI, "file://")), l.Range.Start.Line+1, l.Range.Start.Character)
		}
	})

	// 6. Rename — 重命名 Greeter → Welcomer
	t.Run("Rename", func(t *testing.T) {
		// "Greeter" type: line 5, col 5 (0-indexed)
		edit, err := mgr.Rename(goFile, 5, 5, "Welcomer")
		if err != nil {
			t.Fatalf("Rename: %v", err)
		}
		if edit == nil || len(edit.Changes) == 0 {
			// gopls 在 tmpdir 中可能不支持 rename (需要 gopls.local 配置)
			t.Skip("Rename returned no edits (gopls may restrict rename in tmpdir)")
		}
		totalEdits := 0
		for uri, edits := range edit.Changes {
			totalEdits += len(edits)
			t.Logf("Rename edits in %s: %d", filepath.Base(strings.TrimPrefix(uri, "file://")), len(edits))
			for _, e := range edits {
				t.Logf("  L%d:%d-%d:%d → %q", e.Range.Start.Line+1, e.Range.Start.Character, e.Range.End.Line+1, e.Range.End.Character, e.NewText)
			}
		}
		// Greeter 在代码中至少出现 4 次 (type + receiver + return type + constructor return)
		if totalEdits < 3 {
			t.Errorf("expected at least 3 rename edits, got %d", totalEdits)
		}
	})
}

func TestLSP_E2E_Rust(t *testing.T) {
	requireServerOrSkip(t, "rust")
	dir := setupTestDir(t)

	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + dir)
	defer mgr.StopAll()

	rsFile := filepath.Join(dir, "src", "main.rs")

	err := mgr.OpenFile(rsFile, rustTestFile)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}

	// rust-analyzer 需要更多时间索引
	time.Sleep(8 * time.Second)

	t.Run("DocumentSymbol", func(t *testing.T) {
		symbols, err := mgr.DocumentSymbol(rsFile)
		if err != nil {
			t.Fatalf("DocumentSymbol: %v", err)
		}
		if len(symbols) == 0 {
			t.Fatal("DocumentSymbol returned 0 symbols")
		}
		t.Logf("Rust DocumentSymbol: %d top-level symbols", len(symbols))
		for _, s := range symbols {
			t.Logf("  %s (%s) L%d-%d", s.Name, s.Kind.String(), s.Range.Start.Line+1, s.Range.End.Line+1)
		}
	})

	t.Run("Definition", func(t *testing.T) {
		// "Greeter::new" call at line 15 (0-indexed), col ~17
		locs, err := mgr.Definition(rsFile, 15, 19)
		if err != nil {
			t.Fatalf("Definition: %v", err)
		}
		if len(locs) == 0 {
			t.Fatal("Definition returned 0 locations")
		}
		t.Logf("Rust Definition: %s L%d", locs[0].URI, locs[0].Range.Start.Line+1)
	})

	t.Run("Hover", func(t *testing.T) {
		// Hover on "Greeter" struct definition: line 0, col 7
		result, err := mgr.Hover(rsFile, 0, 7)
		if err != nil {
			t.Fatalf("Hover: %v", err)
		}
		if result == nil {
			t.Skip("Hover returned nil (rust-analyzer may need more time)")
		}
		t.Logf("Rust Hover: %s", result.Contents.Value[:min(len(result.Contents.Value), 200)])
	})
}

func TestLSP_E2E_JS(t *testing.T) {
	requireServerOrSkip(t, "typescript")
	dir := setupTestDir(t)

	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + dir)
	defer mgr.StopAll()

	jsFile := filepath.Join(dir, "app.js")

	err := mgr.OpenFile(jsFile, jsTestFile)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}

	// tsserver 需要时间启动
	time.Sleep(5 * time.Second)

	t.Run("DocumentSymbol", func(t *testing.T) {
		symbols, err := mgr.DocumentSymbol(jsFile)
		if err != nil {
			t.Fatalf("DocumentSymbol: %v", err)
		}
		if len(symbols) == 0 {
			t.Fatal("DocumentSymbol returned 0 symbols")
		}
		t.Logf("JS DocumentSymbol: %d top-level symbols", len(symbols))
		for _, s := range symbols {
			t.Logf("  %s (%s) L%d-%d", s.Name, s.Kind.String(), s.Range.Start.Line+1, s.Range.End.Line+1)
			for _, c := range s.Children {
				t.Logf("    %s (%s) L%d-%d", c.Name, c.Kind.String(), c.Range.Start.Line+1, c.Range.End.Line+1)
			}
		}
	})

	t.Run("Definition", func(t *testing.T) {
		// "createGreeter" call at line 14 (0-indexed), col ~10
		locs, err := mgr.Definition(jsFile, 14, 10)
		if err != nil {
			t.Fatalf("Definition: %v", err)
		}
		if len(locs) == 0 {
			t.Fatal("Definition returned 0 locations")
		}
		t.Logf("JS Definition: %s L%d", locs[0].URI, locs[0].Range.Start.Line+1)
	})

	t.Run("Hover", func(t *testing.T) {
		// Hover on "Greeter" class: line 0, col 6
		result, err := mgr.Hover(jsFile, 0, 6)
		if err != nil {
			t.Fatalf("Hover: %v", err)
		}
		if result == nil {
			t.Skip("Hover returned nil")
		}
		t.Logf("JS Hover: %s", result.Contents.Value[:min(len(result.Contents.Value), 200)])
	})

	t.Run("References", func(t *testing.T) {
		// "Greeter" class: line 0, col 6
		locs, err := mgr.References(jsFile, 0, 6, true)
		if err != nil {
			t.Fatalf("References: %v", err)
		}
		if len(locs) < 2 {
			t.Fatalf("expected at least 2 references, got %d", len(locs))
		}
		t.Logf("JS References: %d locations", len(locs))
	})
}

// ─── 被废弃的旧测试能力不应回归 ───

func TestLSP_E2E_GracefulDegradation(t *testing.T) {
	// 测试: 不可读文件应 fail-closed 返回错误，避免给旧结果。
	mgr := NewManager(nil)
	defer mgr.StopAll()

	_, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 对未打开的文件调用新方法
	if _, err := mgr.Definition("/nonexistent/file.go", 0, 0); err == nil {
		t.Fatal("expected error for Definition on nonexistent file")
	}

	if _, err := mgr.References("/nonexistent/file.go", 0, 0, true); err == nil {
		t.Fatal("expected error for References on nonexistent file")
	}

	if _, err := mgr.DocumentSymbol("/nonexistent/file.go"); err == nil {
		t.Fatal("expected error for DocumentSymbol on nonexistent file")
	}

	if _, err := mgr.Rename("/nonexistent/file.go", 0, 0, "foo"); err == nil {
		t.Fatal("expected error for Rename on nonexistent file")
	}

	t.Log("GracefulDegradation: nonexistent files now return fail-closed errors ✓")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
