package redis_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	goRedisImportPath    = "github.com/redis/go-redis/v9"
	infraRedisImportPath = "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
)

// findModuleRoot remonte dynamiquement l'arborescence jusqu'à go.mod afin de ne dépendre d'aucun cwd fixe.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "unable to resolve caller path")

	dir := filepath.Dir(currentFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found while walking upwards from %s", currentFile)
		}
		dir = parent
	}
}

// isRedisImportAllowed applique la whitelist DDD stricte pour les imports Redis bas niveau.
func isRedisImportAllowed(relSlashPath, importPath string) bool {
	relSlashPath = filepath.ToSlash(strings.TrimPrefix(relSlashPath, "./"))

	switch importPath {
	case goRedisImportPath:
		return strings.HasPrefix(relSlashPath, "internal/infrastructure/redis/") ||
			strings.HasPrefix(relSlashPath, "internal/repository/redis/") ||
			strings.HasPrefix(relSlashPath, "internal/infrastructure/test/")
	case infraRedisImportPath:
		return relSlashPath == "cmd/main.go" ||
			strings.HasPrefix(relSlashPath, "internal/infrastructure/redis/") ||
			strings.HasPrefix(relSlashPath, "internal/repository/redis/") ||
			strings.HasPrefix(relSlashPath, "internal/infrastructure/test/")
	default:
		return true
	}
}

func collectGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" || name == "k8s" || name == ".idea" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func findForbiddenRedisImports(root string) ([]string, error) {
	files, err := collectGoFiles(root)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	var violations []string

	for _, absPath := range files {
		relPath, errRel := filepath.Rel(root, absPath)
		if errRel != nil {
			return nil, errRel
		}
		relSlash := filepath.ToSlash(relPath)

		parsed, errParse := parser.ParseFile(fset, absPath, nil, parser.ImportsOnly)
		if errParse != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", relSlash, errParse)
		}

		for _, imp := range parsed.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath != goRedisImportPath && importPath != infraRedisImportPath {
				continue
			}
			if !isRedisImportAllowed(relSlash, importPath) {
				violations = append(violations, fmt.Sprintf("%s imports forbidden package %q", relSlash, importPath))
			}
		}
	}

	sort.Strings(violations)
	return violations, nil
}

func exprUsesForbiddenSelector(expr ast.Expr, forbiddenAliases map[string]string) (string, bool) {
	if expr == nil {
		return "", false
	}
	var leaked string
	ast.Inspect(expr, func(n ast.Node) bool {
		if leaked != "" {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if pkgPath, forbidden := forbiddenAliases[ident.Name]; forbidden {
			leaked = fmt.Sprintf("%s.%s (%s)", ident.Name, sel.Sel.Name, pkgPath)
			return false
		}
		return true
	})
	return leaked, leaked != ""
}

func findExportedRedisAPILeaks(repoDir string) ([]string, error) {
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return nil, err
	}

	var goFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			goFiles = append(goFiles, filepath.Join(repoDir, name))
		}
	}
	sort.Strings(goFiles)

	fset := token.NewFileSet()
	var leaks []string

	for _, absPath := range goFiles {
		fileName := filepath.Base(absPath)
		parsed, errParse := parser.ParseFile(fset, absPath, nil, 0)
		if errParse != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", fileName, errParse)
		}

		forbiddenAliases := make(map[string]string)
		for _, imp := range parsed.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath != goRedisImportPath && importPath != infraRedisImportPath {
				continue
			}
			alias := "redis"
			if imp.Name != nil && imp.Name.Name != "" && imp.Name.Name != "_" && imp.Name.Name != "." {
				alias = imp.Name.Name
			}
			forbiddenAliases[alias] = importPath
		}

		for _, decl := range parsed.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if !d.Name.IsExported() {
					continue
				}
				if d.Recv != nil && len(d.Recv.List) > 0 {
					// Vérifie que Collection n'expose pas de méthode Pipeline() ou Client()
					recvText := formatRecvType(d.Recv.List[0].Type)
					if recvText == "Collection" && (d.Name.Name == "Pipeline" || d.Name.Name == "Client") {
						leaks = append(leaks, fmt.Sprintf("%s: Collection must not export %s()", fileName, d.Name.Name))
					}
				}
				if d.Type.Params != nil {
					for _, field := range d.Type.Params.List {
						if leaked, ok := exprUsesForbiddenSelector(field.Type, forbiddenAliases); ok {
							leaks = append(leaks, fmt.Sprintf("%s: exported func %s param leaks %s", fileName, d.Name.Name, leaked))
						}
					}
				}
				if d.Type.Results != nil {
					for _, field := range d.Type.Results.List {
						if leaked, ok := exprUsesForbiddenSelector(field.Type, forbiddenAliases); ok {
							leaks = append(leaks, fmt.Sprintf("%s: exported func %s return leaks %s", fileName, d.Name.Name, leaked))
						}
					}
				}

			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if !s.Name.IsExported() {
							continue
						}
						if st, ok := s.Type.(*ast.StructType); ok && st.Fields != nil {
							for _, field := range st.Fields.List {
								// Vérifie que Collection ne possède aucun champ Client exporté
								if s.Name.Name == "Collection" {
									for _, fieldName := range field.Names {
										if fieldName.Name == "Client" {
											leaks = append(leaks, fmt.Sprintf("%s: Collection must not export field Client", fileName))
										}
									}
								}
								isExportedField := len(field.Names) == 0
								for _, fieldName := range field.Names {
									if fieldName.IsExported() {
										isExportedField = true
									}
								}
								if !isExportedField {
									continue
								}
								if leaked, ok := exprUsesForbiddenSelector(field.Type, forbiddenAliases); ok {
									leaks = append(leaks, fmt.Sprintf("%s: exported struct %s leaks %s", fileName, s.Name.Name, leaked))
								}
							}
						} else if leaked, ok := exprUsesForbiddenSelector(s.Type, forbiddenAliases); ok {
							leaks = append(leaks, fmt.Sprintf("%s: exported type %s leaks %s", fileName, s.Name.Name, leaked))
						}

					case *ast.ValueSpec:
						hasExported := false
						for _, name := range s.Names {
							if name.IsExported() {
								hasExported = true
							}
						}
						if !hasExported {
							continue
						}
						if leaked, ok := exprUsesForbiddenSelector(s.Type, forbiddenAliases); ok {
							leaks = append(leaks, fmt.Sprintf("%s: exported value %v leaks %s", fileName, s.Names, leaked))
						}
					}
				}
			}
		}
	}

	sort.Strings(leaks)
	return leaks, nil
}

func formatRecvType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return formatRecvType(t.X)
	case *ast.Ident:
		return t.Name
	default:
		return ""
	}
}

func TestRedisArchitectureGuard(t *testing.T) {
	root := findModuleRoot(t)

	t.Run("StrictImportWhitelist", func(t *testing.T) {
		violations, err := findForbiddenRedisImports(root)
		require.NoError(t, err)
		assert.Empty(t, violations, "Forbidden Redis imports detected outside DDD whitelist:\n%s", strings.Join(violations, "\n"))
	})

	t.Run("ZeroExportedRepositoryTypeLeaks", func(t *testing.T) {
		repoDir := filepath.Join(root, "internal", "repository", "redis")
		leaks, err := findExportedRedisAPILeaks(repoDir)
		require.NoError(t, err)
		assert.Empty(t, leaks, "Exported API leaks detected in internal/repository/redis:\n%s", strings.Join(leaks, "\n"))
	})
}

func TestRedisArchitectureGuard_WhitelistRules(t *testing.T) {
	cases := []struct {
		name       string
		relPath    string
		importPath string
		allowed    bool
	}{
		// Allowed locations
		{"cmd_main_infra", "cmd/main.go", infraRedisImportPath, true},
		{"infra_redis_goredis", "internal/infrastructure/redis/client.go", goRedisImportPath, true},
		{"infra_redis_test_goredis", "internal/infrastructure/redis/client_test.go", goRedisImportPath, true},
		{"repo_redis_goredis", "internal/repository/redis/batch.go", goRedisImportPath, true},
		{"repo_redis_infra", "internal/repository/redis/collections.go", infraRedisImportPath, true},
		{"infra_test_goredis", "internal/infrastructure/test/redis_fake.go", goRedisImportPath, true},
		{"infra_test_infra", "internal/infrastructure/test/redis_fake.go", infraRedisImportPath, true},

		// Forbidden locations (production & tests)
		{"cmd_main_goredis_forbidden", "cmd/main.go", goRedisImportPath, false},
		{"handler_goredis_forbidden", "internal/api/handlers/post_handlers/create.go", goRedisImportPath, false},
		{"handler_infra_forbidden", "internal/api/handlers/post_handlers/create.go", infraRedisImportPath, false},
		{"handler_test_infra_forbidden", "internal/api/handlers/report_handlers/report_validation_test.go", infraRedisImportPath, false},
		{"ws_hub_goredis_forbidden", "internal/api/websocket/hub.go", goRedisImportPath, false},
		{"ws_hub_infra_forbidden", "internal/api/websocket/hub.go", infraRedisImportPath, false},
		{"service_goredis_forbidden", "internal/service/cache_service/session_cache_service.go", goRedisImportPath, false},
		{"service_infra_forbidden", "internal/service/cache_service/variables_cache_service.go", infraRedisImportPath, false},
		{"service_test_goredis_forbidden", "internal/service/cache_service/object_cache_service/object_cache_article_service_test.go", goRedisImportPath, false},
		{"service_test_infra_forbidden", "internal/service/cluster_service/registry_test.go", infraRedisImportPath, false},
		{"worker_goredis_forbidden", "internal/worker/email_worker.go", goRedisImportPath, false},
		{"worker_infra_forbidden", "internal/worker/cron_lock.go", infraRedisImportPath, false},
		{"worker_test_infra_forbidden", "internal/worker/poi_review_worker_test.go", infraRedisImportPath, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.allowed, isRedisImportAllowed(tc.relPath, tc.importPath))
		})
	}
}

func TestRedisArchitectureGuard_DetectsViolations(t *testing.T) {
	t.Run("DetectsForbiddenImportsInTempTree", func(t *testing.T) {
		tmp := t.TempDir()
		badDir := filepath.Join(tmp, "internal", "service", "bad_service")
		require.NoError(t, os.MkdirAll(badDir, 0o755))
		badFile := filepath.Join(badDir, "leak.go")
		require.NoError(t, os.WriteFile(badFile, []byte(`package bad_service

import (
	_ "github.com/redis/go-redis/v9"
	_ "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
)
`), 0o600))

		violations, err := findForbiddenRedisImports(tmp)
		require.NoError(t, err)
		require.Len(t, violations, 2)
		assert.Contains(t, violations[0], "internal/service/bad_service/leak.go")
		assert.Contains(t, violations[1], "internal/service/bad_service/leak.go")
	})

	t.Run("DetectsExportedRepositoryAPILeaks", func(t *testing.T) {
		tmp := t.TempDir()
		badRepoFile := filepath.Join(tmp, "bad_repo.go")
		require.NoError(t, os.WriteFile(badRepoFile, []byte(`package redis

import goredis "github.com/redis/go-redis/v9"

type Collection struct {
	Client goredis.UniversalClient
}

func (c *Collection) Pipeline() goredis.Pipeliner { return nil }
func LeakyFunc() *goredis.StringCmd { return nil }
`), 0o600))

		leaks, err := findExportedRedisAPILeaks(tmp)
		require.NoError(t, err)
		assert.NotEmpty(t, leaks)
		joined := strings.Join(leaks, "\n")
		assert.Contains(t, joined, "Collection must not export field Client")
		assert.Contains(t, joined, "Collection must not export Pipeline()")
		assert.Contains(t, joined, "LeakyFunc")
	})
}
