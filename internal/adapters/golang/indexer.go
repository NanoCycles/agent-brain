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
	"regexp"
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
		if d.IsDir() {
			return nil
		}
		file, nodes, rels, err := parseFile(repoRoot, rel, path, repo, now)
		if err != nil {
			return err
		}
		if file.Path == "" {
			return nil
		}
		index.Files = append(index.Files, file)
		index.Nodes = append(index.Nodes, nodes...)
		index.Relations = append(index.Relations, rels...)
		return nil
	})
	return index, err
}

func parseFile(repoRoot, rel, abs string, repo domain.Repository, now time.Time) (domain.IndexedFile, []domain.GraphNode, []domain.GraphRelationship, error) {
	lower := strings.ToLower(rel)
	switch {
	case strings.HasSuffix(lower, ".go"):
		return parseGoFile(repoRoot, rel, abs, repo, now)
	case strings.HasSuffix(lower, ".graphql"), strings.HasSuffix(lower, ".graphqls"):
		return parseGraphQLSchemaFile(rel, abs, repo, now)
	case strings.HasSuffix(lower, ".proto"):
		return parseProtoFile(rel, abs, repo, now)
	default:
		return domain.IndexedFile{}, nil, nil, nil
	}
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
	if !strings.HasSuffix(strings.ToLower(rel), "_test.go") {
		contracts := detectGoContracts(rel, string(src), parsed, file)
		file.Contracts = append(file.Contracts, contracts...)
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
	appendContractNodes(&nodes, &rels, rel, repo, parsed.Name.Name, layer, now, file.Contracts)
	return file, nodes, rels, nil
}

func parseGraphQLSchemaFile(rel, abs string, repo domain.Repository, now time.Time) (domain.IndexedFile, []domain.GraphNode, []domain.GraphRelationship, error) {
	src, err := os.ReadFile(abs)
	if err != nil {
		return domain.IndexedFile{}, nil, nil, err
	}
	layer := DetectLayer(rel)
	file := domain.IndexedFile{Path: rel, Package: "graphql", Layer: layer, Hash: hash(src), IndexedAt: now}
	file.Contracts = detectGraphQLSchemaContracts(rel, string(src))
	fileNode := node("File", rel, rel, repo, file.Package, layer, now)
	layerNode := node("Layer", layer, layer, repo, "", layer, now)
	nodes := []domain.GraphNode{fileNode, layerNode}
	rels := []domain.GraphRelationship{
		relate("Repository", repo.Root, "File", rel, "CONTAINS", repo.Root),
		relate("File", rel, "Layer", layer, "BELONGS_TO_LAYER", repo.Root),
	}
	appendContractNodes(&nodes, &rels, rel, repo, file.Package, layer, now, file.Contracts)
	return file, nodes, rels, nil
}

func parseProtoFile(rel, abs string, repo domain.Repository, now time.Time) (domain.IndexedFile, []domain.GraphNode, []domain.GraphRelationship, error) {
	src, err := os.ReadFile(abs)
	if err != nil {
		return domain.IndexedFile{}, nil, nil, err
	}
	layer := DetectLayer(rel)
	file := domain.IndexedFile{Path: rel, Package: "proto", Layer: layer, Hash: hash(src), IndexedAt: now}
	file.Contracts = detectProtoContracts(rel, string(src))
	fileNode := node("File", rel, rel, repo, file.Package, layer, now)
	layerNode := node("Layer", layer, layer, repo, "", layer, now)
	nodes := []domain.GraphNode{fileNode, layerNode}
	rels := []domain.GraphRelationship{
		relate("Repository", repo.Root, "File", rel, "CONTAINS", repo.Root),
		relate("File", rel, "Layer", layer, "BELONGS_TO_LAYER", repo.Root),
	}
	appendContractNodes(&nodes, &rels, rel, repo, file.Package, layer, now, file.Contracts)
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
	case strings.Contains(p, "internal/adapters/graphql"), strings.Contains(p, "graph/"), strings.Contains(p, "graphql"):
		return "adapter_graphql"
	case strings.Contains(p, "internal/adapters/grpc"), strings.Contains(p, "grpc"), strings.Contains(p, "proto"):
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

func detectGoContracts(rel, src string, parsed *ast.File, file domain.IndexedFile) []domain.Contract {
	path := filepath.ToSlash(strings.ToLower(rel))
	hay := path + " " + strings.ToLower(file.Package) + " " + strings.Join(file.Imports, " ") + " " + strings.ToLower(src)
	var out []domain.Contract
	if strings.Contains(hay, "graphql") || strings.Contains(hay, "gqlgen") || strings.Contains(hay, "resolver") || strings.Contains(path, "graph/") {
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			recv := receiverName(fn.Recv)
			name := fn.Name.Name
			if strings.Contains(strings.ToLower(recv+" "+name), "resolver") || strings.Contains(path, "resolver") || strings.Contains(path, "graphql") {
				out = append(out, domain.Contract{Kind: "GraphQLField", Name: strings.TrimPrefix(recv+"."+name, "."), Path: rel, Operation: "resolve", Evidence: "GraphQL resolver function or method", Confidence: 0.85})
			}
		}
	}
	out = append(out, detectRESTContracts(rel, src)...)
	out = append(out, detectEventContracts(rel, parsed, hay)...)
	out = append(out, detectPersistenceContracts(rel, parsed, hay)...)
	return out
}

func detectGraphQLSchemaContracts(rel, src string) []domain.Contract {
	var out []domain.Contract
	typeRe := regexp.MustCompile(`(?m)^\s*(type|interface|input|enum)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	fieldRe := regexp.MustCompile(`(?m)^\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:\([^)]*\))?\s*:`)
	currentType := ""
	for _, line := range strings.Split(src, "\n") {
		if m := typeRe.FindStringSubmatch(line); len(m) == 3 {
			currentType = m[2]
			out = append(out, domain.Contract{Kind: "Contract", Name: "GraphQLType." + currentType, Path: rel, Operation: m[1], Evidence: "GraphQL schema type declaration", Confidence: 0.95})
			continue
		}
		if currentType != "" {
			if strings.Contains(line, "}") {
				currentType = ""
				continue
			}
			if m := fieldRe.FindStringSubmatch(line); len(m) == 2 {
				out = append(out, domain.Contract{Kind: "GraphQLField", Name: currentType + "." + m[1], Path: rel, Operation: "field", Evidence: "GraphQL schema field", Confidence: 0.95})
			}
		}
	}
	return out
}

func detectProtoContracts(rel, src string) []domain.Contract {
	var out []domain.Contract
	serviceRe := regexp.MustCompile(`(?m)^\s*service\s+([A-Za-z_][A-Za-z0-9_]*)`)
	rpcRe := regexp.MustCompile(`(?m)^\s*rpc\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	currentService := ""
	for _, line := range strings.Split(src, "\n") {
		if m := serviceRe.FindStringSubmatch(line); len(m) == 2 {
			currentService = m[1]
			out = append(out, domain.Contract{Kind: "Contract", Name: "GRPCService." + currentService, Path: rel, Operation: "service", Evidence: "protobuf service declaration", Confidence: 0.95})
			continue
		}
		if strings.Contains(line, "}") {
			currentService = ""
		}
		if m := rpcRe.FindStringSubmatch(line); len(m) == 2 {
			name := m[1]
			if currentService != "" {
				name = currentService + "." + name
			}
			out = append(out, domain.Contract{Kind: "GRPCMethod", Name: name, Path: rel, Operation: "rpc", Evidence: "protobuf rpc declaration", Confidence: 0.95})
		}
	}
	return out
}

func detectRESTContracts(rel, src string) []domain.Contract {
	var out []domain.Contract
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(GET|POST|PUT|PATCH|DELETE)\s+["` + "`" + `]([^"` + "`" + `]+)["` + "`" + `]`),
		regexp.MustCompile(`(?i)\.(Get|Post|Put|Patch|Delete|Handle|HandleFunc)\s*\(\s*["` + "`" + `]([^"` + "`" + `]+)["` + "`" + `]`),
		regexp.MustCompile(`http\.HandleFunc\s*\(\s*["` + "`" + `]([^"` + "`" + `]+)["` + "`" + `]`),
	}
	for _, re := range patterns {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			method := "HTTP"
			route := ""
			if len(m) == 3 {
				method = strings.ToUpper(m[1])
				route = m[2]
			} else if len(m) == 2 {
				route = m[1]
			}
			if route != "" && strings.HasPrefix(route, "/") {
				out = append(out, domain.Contract{Kind: "RESTEndpoint", Name: method + " " + route, Path: rel, Operation: method, Evidence: "REST route registration", Confidence: 0.85})
			}
		}
	}
	return out
}

func detectEventContracts(rel string, parsed *ast.File, hay string) []domain.Contract {
	if !(strings.Contains(hay, "event") || strings.Contains(hay, "consumer") || strings.Contains(hay, "producer") || strings.Contains(hay, "kafka") || strings.Contains(hay, "pubsub")) {
		return nil
	}
	var out []domain.Contract
	for _, decl := range parsed.Decls {
		if ts, ok := typeSpec(decl); ok {
			name := strings.ToLower(ts.Name.Name)
			if strings.Contains(name, "event") || strings.Contains(name, "message") || strings.Contains(name, "consumer") || strings.Contains(name, "producer") {
				kind := "HANDLES"
				if strings.Contains(name, "producer") || strings.Contains(name, "published") {
					kind = "PUBLISHES"
				}
				out = append(out, domain.Contract{Kind: "EventType", Name: ts.Name.Name, Path: rel, Operation: kind, Evidence: "event-related type declaration", Confidence: 0.75})
			}
		}
	}
	return out
}

func detectPersistenceContracts(rel string, parsed *ast.File, hay string) []domain.Contract {
	if !(strings.Contains(hay, "repository") || strings.Contains(hay, "store") || strings.Contains(hay, "dao") || strings.Contains(hay, "postgres") || strings.Contains(hay, "mysql") || strings.Contains(hay, "sqlite")) {
		return nil
	}
	var out []domain.Contract
	for _, decl := range parsed.Decls {
		if ts, ok := typeSpec(decl); ok {
			name := strings.ToLower(ts.Name.Name)
			if strings.Contains(name, "repository") || strings.Contains(name, "store") || strings.Contains(name, "dao") {
				out = append(out, domain.Contract{Kind: "Contract", Name: "DB." + ts.Name.Name, Path: rel, Operation: "persistence", Evidence: "repository/store/dao type declaration", Confidence: 0.7})
			}
		}
	}
	return out
}

func typeSpec(decl ast.Decl) (*ast.TypeSpec, bool) {
	gen, ok := decl.(*ast.GenDecl)
	if !ok {
		return nil, false
	}
	for _, spec := range gen.Specs {
		if ts, ok := spec.(*ast.TypeSpec); ok {
			return ts, true
		}
	}
	return nil, false
}

func appendContractNodes(nodes *[]domain.GraphNode, rels *[]domain.GraphRelationship, filePath string, repo domain.Repository, pkg, layer string, now time.Time, contracts []domain.Contract) {
	for _, c := range contracts {
		label := c.Kind
		if label == "" {
			label = "Contract"
		}
		key := filePath + "#contract:" + c.Kind + ":" + c.Name
		n := node(label, c.Name, key, repo, pkg, layer, now)
		n.Source = "contract-extractor"
		n.Confidence = c.Confidence
		n.Properties = map[string]any{"operation": c.Operation, "evidence": c.Evidence}
		*nodes = append(*nodes, n)
		relType := "EXPOSES"
		switch c.Kind {
		case "EventType":
			if c.Operation == "PUBLISHES" {
				relType = "PUBLISHES"
			} else {
				relType = "HANDLES"
			}
		case "GRPCMethod", "RESTEndpoint", "GraphQLField":
			relType = "EXPOSES"
		}
		*rels = append(*rels, relate("File", filePath, label, key, relType, repo.Root))
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
