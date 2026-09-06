package contextengine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type goSymbol struct {
	Name string
	Line int
}

// goFileAnalysis is retained as an internal compatibility name, but now holds
// the lightweight syntax index shared by Go, Python and JS/TS. Go remains
// compiler-parser backed; the other languages use bounded declaration/import
// scanners so MAR stays CGO-free and portable on the V1 Windows host.
type goFileAnalysis struct {
	Language string
	Symbols  []goSymbol
	Imports  []string
}

type analysisCache struct {
	mu     sync.Mutex
	max    int
	values map[string]goFileAnalysis
	order  []string
}

func newAnalysisCache(maxEntries int) *analysisCache {
	return &analysisCache{max: maxEntries, values: make(map[string]goFileAnalysis)}
}

func (c *analysisCache) getOrCompute(hash string, compute func() goFileAnalysis) goFileAnalysis {
	c.mu.Lock()
	if value, ok := c.values[hash]; ok {
		c.mu.Unlock()
		return value
	}
	c.mu.Unlock()

	computed := compute()
	c.mu.Lock()
	defer c.mu.Unlock()
	if value, ok := c.values[hash]; ok {
		return value
	}
	if c.max <= 0 {
		return computed
	}
	for len(c.values) >= c.max && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.values, oldest)
	}
	c.values[hash] = computed
	c.order = append(c.order, hash)
	return computed
}

func (c *analysisCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.values)
}

func (c *analysisCache) clear() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := len(c.values)
	c.values = make(map[string]goFileAnalysis)
	c.order = nil
	return removed
}

func analyzeCodeFile(path string, source []byte) goFileAnalysis {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return analyzeGoFile(path, source)
	case ".py", ".pyw":
		return analyzePythonFile(source)
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		return analyzeJavaScriptFile(source)
	default:
		return goFileAnalysis{}
	}
}

func analyzeGoFile(path string, source []byte) goFileAnalysis {
	if !strings.EqualFold(filepath.Ext(path), ".go") {
		return goFileAnalysis{}
	}
	fset := token.NewFileSet()
	file, _ := parser.ParseFile(fset, path, source, parser.SkipObjectResolution)
	if file == nil {
		return goFileAnalysis{}
	}
	analysis := goFileAnalysis{Language: "go"}
	for _, decl := range file.Decls {
		switch node := decl.(type) {
		case *ast.FuncDecl:
			if node.Name != nil {
				analysis.Symbols = append(analysis.Symbols, goSymbol{Name: node.Name.Name, Line: fset.Position(node.Name.Pos()).Line})
			}
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				switch value := spec.(type) {
				case *ast.TypeSpec:
					if value.Name != nil {
						analysis.Symbols = append(analysis.Symbols, goSymbol{Name: value.Name.Name, Line: fset.Position(value.Name.Pos()).Line})
					}
				case *ast.ValueSpec:
					for _, name := range value.Names {
						analysis.Symbols = append(analysis.Symbols, goSymbol{Name: name.Name, Line: fset.Position(name.Pos()).Line})
					}
				}
			}
		}
	}
	for _, imp := range file.Imports {
		value, err := strconv.Unquote(imp.Path.Value)
		if err == nil && strings.TrimSpace(value) != "" {
			analysis.Imports = append(analysis.Imports, value)
		}
	}
	return normalizeAnalysis(analysis)
}

var (
	pythonDeclRE   = regexp.MustCompile(`^(?:async\s+def|def|class)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	pythonFromRE   = regexp.MustCompile(`^from\s+([.A-Za-z_][.A-Za-z0-9_]*)\s+import\s+`)
	pythonImportRE = regexp.MustCompile(`^import\s+(.+)$`)

	jsDeclRE             = regexp.MustCompile(`^(?:(?:export\s+)?(?:default\s+)?)?(?:async\s+)?(?:function|class|interface|type|enum|const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	jsFromRE             = regexp.MustCompile(`^(?:import|export)\b.*?\bfrom\s*["']([^"']+)["']`)
	jsSideEffectImportRE = regexp.MustCompile(`^import\s*["']([^"']+)["']`)
	jsCallImportRE       = regexp.MustCompile(`(?:require|import)\s*\(\s*["']([^"']+)["']\s*\)`)
)

func analyzePythonFile(source []byte) goFileAnalysis {
	analysis := goFileAnalysis{Language: "python"}
	for i, raw := range strings.Split(string(source), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if match := pythonDeclRE.FindStringSubmatch(line); len(match) == 2 {
			analysis.Symbols = append(analysis.Symbols, goSymbol{Name: match[1], Line: i + 1})
		}
		if match := pythonFromRE.FindStringSubmatch(line); len(match) == 2 {
			analysis.Imports = append(analysis.Imports, match[1])
			continue
		}
		if match := pythonImportRE.FindStringSubmatch(line); len(match) == 2 {
			for _, item := range strings.Split(match[1], ",") {
				fields := strings.Fields(strings.TrimSpace(item))
				if len(fields) > 0 {
					analysis.Imports = append(analysis.Imports, fields[0])
				}
			}
		}
	}
	return normalizeAnalysis(analysis)
}

func analyzeJavaScriptFile(source []byte) goFileAnalysis {
	analysis := goFileAnalysis{Language: "javascript"}
	for i, raw := range strings.Split(string(source), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if match := jsDeclRE.FindStringSubmatch(line); len(match) == 2 {
			analysis.Symbols = append(analysis.Symbols, goSymbol{Name: match[1], Line: i + 1})
		}
		if match := jsFromRE.FindStringSubmatch(line); len(match) == 2 {
			analysis.Imports = append(analysis.Imports, match[1])
		}
		if match := jsSideEffectImportRE.FindStringSubmatch(line); len(match) == 2 {
			analysis.Imports = append(analysis.Imports, match[1])
		}
		for _, match := range jsCallImportRE.FindAllStringSubmatch(line, -1) {
			if len(match) == 2 {
				analysis.Imports = append(analysis.Imports, match[1])
			}
		}
	}
	return normalizeAnalysis(analysis)
}

func normalizeAnalysis(analysis goFileAnalysis) goFileAnalysis {
	sort.Slice(analysis.Symbols, func(i, j int) bool {
		if analysis.Symbols[i].Line != analysis.Symbols[j].Line {
			return analysis.Symbols[i].Line < analysis.Symbols[j].Line
		}
		return analysis.Symbols[i].Name < analysis.Symbols[j].Name
	})
	sort.Strings(analysis.Imports)
	analysis.Imports = dedupeStrings(analysis.Imports)
	return analysis
}

func dedupeStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:0]
	var last string
	for _, value := range values {
		if len(out) > 0 && value == last {
			continue
		}
		out = append(out, value)
		last = value
	}
	return out
}
