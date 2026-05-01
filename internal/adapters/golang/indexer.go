package golang

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/NanoCycles/agent-brain/internal/adapters/filesystem"
	"github.com/NanoCycles/agent-brain/internal/domain"
)

type Indexer struct{}

func (Indexer) Index(ctx context.Context, repoRoot string) (domain.CodeIndex, error) {
	goMod := filepath.Join(repoRoot, "go.mod")
	data, err := os.ReadFile(goMod)
	if err != nil {
		return domain.CodeIndex{}, err
	}
	module := parseModule(data)
	now := time.Now().UTC()
	repo := domain.Repository{
		Name: filepath.Base(repoRoot), Root: repoRoot, GoModule: module,
		CommitSHA: commitSHA(ctx, repoRoot), IndexedAt: now,
	}
	index := domain.CodeIndex{Repository: repo}
	repoNode := domain.GraphNode{Label: "Repository", Name: repo.Name, Path: repo.Root, Repo: repo.Root, CommitSHA: repo.CommitSHA, IndexedAt: now, Source: "agent-brain", Confidence: 1}
	index.Nodes = append(index.Nodes, repoNode)

	err = filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repoRoot, path)
		if rel == "." {
			return nil
		}
		if filesystem.IsForbiddenPath(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		file, nodes, rels, err := parseGoFile(repoRoot, rel, path, repo, now)
		if err != nil {
			return err
		}
		index.Files = append(index.Files, file)
		index.Nodes = append(index.Nodes, nodes...)
		index.Relations = append(index.Relations, rels...)
		return nil
	})
	return index, err
}

func parseGoFile(repoRoot, rel, abs string, repo domain.Repository, now time.Time) (domain.IndexedFile, []domain.GraphNode, []domain.GraphRelationship, error) {
	src, err := os.ReadFile(abs)
	if err != nil {
		return domain.IndexedFile{}, nil, nil, err
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, abs, src, parser.ParseComments)
	if err != nil {
		return domain.IndexedFile{}, nil, nil, err
	}
	layer := DetectLayer(rel)
	file := domain.IndexedFile{Path: rel, Package: parsed.Name.Name, Layer: layer, Hash: hash(src), IndexedAt: now}
	for _, im := range parsed.Imports {
		file.Imports = append(file.Imports, strings.Trim(im.Path.Value, `"`))
	}
	fileNode := node("File", rel, rel, repo, parsed.Name.Name, layer, now)
	pkgPath := filepath.Dir(rel)
	if pkgPath == "." {
		pkgPath = parsed.Name.Name
	}
	pkgNode := node("Package", pkgPath, pkgPath, repo, parsed.Name.Name, layer, now)
	layerNode := node("Layer", layer, layer, repo, "", layer, now)
	nodes := []domain.GraphNode{fileNode, pkgNode, layerNode}
	rels := []domain.GraphRelationship{
		relate("Repository", repo.Root, "File", rel, "CONTAINS", repo.Root),
		relate("Package", pkgPath, "File", rel, "CONTAINS", repo.Root),
		relate("File", rel, "Layer", layer, "BELONGS_TO_LAYER", repo.Root),
	}
	for _, decl := range parsed.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				line := fset.Position(ts.Pos()).Line
				switch ts.Type.(type) {
				case *ast.StructType:
					file.Structs = append(file.Structs, domain.Struct{Name: ts.Name.Name, Path: rel, Line: line})
					nodes = append(nodes, node("Struct", ts.Name.Name, rel+"#"+ts.Name.Name, repo, parsed.Name.Name, layer, now))
					rels = append(rels, relate("File", rel, "Struct", rel+"#"+ts.Name.Name, "DEFINES", repo.Root))
				case *ast.InterfaceType:
					file.Interfaces = append(file.Interfaces, domain.Interface{Name: ts.Name.Name, Path: rel, Line: line})
					nodes = append(nodes, node("Interface", ts.Name.Name, rel+"#"+ts.Name.Name, repo, parsed.Name.Name, layer, now))
					rels = append(rels, relate("File", rel, "Interface", rel+"#"+ts.Name.Name, "DEFINES", repo.Root))
				}
			}
		case *ast.FuncDecl:
			line := fset.Position(d.Pos()).Line
			if d.Recv == nil {
				fn := domain.Function{Name: d.Name.Name, Path: rel, Line: line}
				if strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(d.Name.Name, "Test") {
					file.Tests = append(file.Tests, domain.Test{Name: d.Name.Name, Path: rel, Line: line})
					nodes = append(nodes, node("Test", d.Name.Name, rel+"#"+d.Name.Name, repo, parsed.Name.Name, layer, now))
					rels = append(rels, relate("File", rel, "Test", rel+"#"+d.Name.Name, "DEFINES", repo.Root))
				} else {
					file.Functions = append(file.Functions, fn)
					nodes = append(nodes, node("Function", d.Name.Name, rel+"#"+d.Name.Name, repo, parsed.Name.Name, layer, now))
					rels = append(rels, relate("File", rel, "Function", rel+"#"+d.Name.Name, "DEFINES", repo.Root))
				}
			} else {
				recv := receiverName(d.Recv)
				file.Methods = append(file.Methods, domain.Method{Receiver: recv, Name: d.Name.Name, Path: rel, Line: line})
				nodes = append(nodes, node("Method", recv+"."+d.Name.Name, rel+"#"+recv+"."+d.Name.Name, repo, parsed.Name.Name, layer, now))
				rels = append(rels, relate("File", rel, "Method", rel+"#"+recv+"."+d.Name.Name, "DEFINES", repo.Root))
			}
		}
	}
	return file, nodes, rels, nil
}

func DetectLayer(path string) string {
	p := filepath.ToSlash(path)
	switch {
	case strings.Contains(p, "internal/domain"):
		return "domain"
	case strings.Contains(p, "internal/application"), strings.Contains(p, "internal/usecase"):
		return "application"
	case strings.Contains(p, "internal/adapters/http"):
		return "adapter_rest"
	case strings.Contains(p, "internal/adapters/graphql"):
		return "adapter_graphql"
	case strings.Contains(p, "internal/adapters/grpc"):
		return "adapter_grpc"
	case strings.Contains(p, "internal/adapters/events"):
		return "adapter_events"
	case strings.Contains(p, "internal/adapters/postgres"), strings.Contains(p, "internal/adapters/memory"):
		return "adapter_persistence"
	case strings.HasPrefix(p, "cmd/"):
		return "entrypoint"
	default:
		return "unknown"
	}
}

func parseModule(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func receiverName(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	switch t := fl.List[0].Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

func node(label, name, path string, repo domain.Repository, pkg, layer string, now time.Time) domain.GraphNode {
	return domain.GraphNode{Label: label, Name: name, Path: path, Repo: repo.Root, Package: pkg, Layer: layer, CommitSHA: repo.CommitSHA, IndexedAt: now, Source: "go/parser", Confidence: 0.9}
}

func relate(fromLabel, from, toLabel, to, typ, repo string) domain.GraphRelationship {
	return domain.GraphRelationship{FromLabel: fromLabel, FromKey: from, ToLabel: toLabel, ToKey: to, Type: typ, Repo: repo}
}

func hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func commitSHA(ctx context.Context, root string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
