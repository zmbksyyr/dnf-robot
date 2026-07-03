package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var forbiddenFileNameTokens = []string{
	"compat",
	"temp",
	"legacy",
	"bridge",
	"adapter",
	"misc",
	"helper",
}

var sqlImportAllowedDirs = []string{
	"cmd/robot",
	"internal/foundation/sql",
	"internal/foundation/dbstatus",
	"internal/scheduler",
	"internal/capability/marketapp",
	"internal/protocol/dnf",
}

func TestMutexDeclarationsStayInsideLockhub(t *testing.T) {
	root := repoRoot(t)
	internal := filepath.Join(root, "internal")
	err := filepath.WalkDir(internal, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.Contains(filepath.ToSlash(path), "/internal/foundation/lockhub/") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		mutexToken := "sync" + "." + "Mutex"
		rwMutexToken := "sync" + "." + "RWMutex"
		if strings.Contains(text, mutexToken) || strings.Contains(text, rwMutexToken) {
			t.Errorf("%s declares raw mutex outside lockhub", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", internal, err)
	}
}

func TestSQLPackageImportsStayInRepositoryOrProtocolCode(t *testing.T) {
	root := repoRoot(t)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) != "database/sql" {
				continue
			}
			if !pathUnderAny(root, path, sqlImportAllowedDirs) {
				t.Errorf("%s imports database/sql outside approved SQL boundary", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

func TestGoFileNamesDoNotUseTemporaryStructureNames(t *testing.T) {
	root := repoRoot(t)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), ".go"))
		for _, token := range forbiddenFileNameTokens {
			if base == token || strings.HasPrefix(base, token+"_") || strings.HasSuffix(base, "_"+token) || strings.Contains(base, "_"+token+"_") {
				t.Errorf("%s uses temporary structure token %q in file name", path, token)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

func TestReadmeFilesDoNotFragmentDocumentation(t *testing.T) {
	root := repoRoot(t)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Base(path), "README.md") {
			t.Errorf("%s fragments documentation; use doc/规整文档.md", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

func TestSchedulerDoesNotExposeActionFacadeMethods(t *testing.T) {
	root := repoRoot(t)
	schedulerDir := filepath.Join(root, "internal", "scheduler")
	forbidden := map[string]bool{
		"Online":          true,
		"OnlineNoConfirm": true,
		"Logout":          true,
		"Move":            true,
		"ShoutOne":        true,
		"Store":           true,
	}
	err := filepath.WalkDir(schedulerDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !forbidden[fn.Name.Name] {
				continue
			}
			for _, field := range fn.Recv.List {
				if receiverIsRobotManager(field.Type) {
					t.Errorf("%s defines RobotManager.%s action facade; use managed command entry or capability service", path, fn.Name.Name)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", schedulerDir, err)
	}
}

func receiverIsRobotManager(expr ast.Expr) bool {
	switch x := expr.(type) {
	case *ast.StarExpr:
		return receiverIsRobotManager(x.X)
	case *ast.Ident:
		return x.Name == "RobotManager"
	default:
		return false
	}
}

func pathUnderAny(root string, path string, dirs []string) bool {
	cleanPath := filepath.ToSlash(filepath.Clean(path))
	for _, dir := range dirs {
		cleanDir := filepath.ToSlash(filepath.Join(root, filepath.FromSlash(dir)))
		if cleanPath == cleanDir || strings.HasPrefix(cleanPath, cleanDir+"/") {
			return true
		}
	}
	return false
}
