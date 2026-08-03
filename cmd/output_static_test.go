package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestApplicationCommandsAvoidDirectProcessWrites keeps application output on
// the Printer boundary. Help/manpage generators use Cobra writers or buffers;
// the one runtime exception is root's gated, once-a-day update notifier.
func TestApplicationCommandsAvoidDirectProcessWrites(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
				if ident, ok := selector.X.(*ast.Ident); ok && ident.Name == "fmt" {
					switch selector.Sel.Name {
					case "Print", "Printf", "Println":
						t.Errorf("%s writes directly to stdout with fmt.%s; use output.Printer", fset.Position(call.Pos()), selector.Sel.Name)
					}
				}
			}
			for _, arg := range call.Args {
				if !isProcessOutput(arg) {
					continue
				}
				// update.Notify owns a gated stderr note and accepts its writer
				// explicitly; all command result paths go through Printer.
				if path == "root.go" && calledSelector(call, "update", "Notify") {
					continue
				}
				t.Errorf("%s passes a process output stream directly; use output.Printer", fset.Position(arg.Pos()))
			}
			return true
		})
	}
}

func isProcessOutput(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	return ok && ident.Name == "os" && (selector.Sel.Name == "Stdout" || selector.Sel.Name == "Stderr")
}

func calledSelector(call *ast.CallExpr, packageName, name string) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != name {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	return ok && ident.Name == packageName
}
