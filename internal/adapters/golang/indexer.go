package golang

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/NanoCycles/agent-brain/internal/adapters/filesystem"
	"github.com/NanoCycles/agent-brain/internal/domain"
	"golang.org/x/tools/go/packages"
)

type Indexer struct{}

type graphTarget struct {
	label string
	key   string
}

type typedTarget struct {
	label string
	key   string
	obj   types.Object
}

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
	if typedRels, ok := inferTypedRelationships(ctx, repoRoot, index.Files, repo.Root); ok {
		index.Relations = append(index.Relations, typedRels...)
	} else {
		index.Relations = append(index.Relations, inferCodeRelationships(index.Files, repo.Root)...)
	}
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
				switch typ := ts.Type.(type) {
				case *ast.StructType:
					file.Structs = append(file.Structs, domain.Struct{Name: ts.Name.Name, Path: rel, Line: line})
					nodes = append(nodes, node("Struct", ts.Name.Name, rel+"#"+ts.Name.Name, repo, parsed.Name.Name, layer, now))
					rels = append(rels, relate("File", rel, "Struct", rel+"#"+ts.Name.Name, "DEFINES", repo.Root))
				case *ast.InterfaceType:
					file.Interfaces = append(file.Interfaces, domain.Interface{Name: ts.Name.Name, Path: rel, Line: line, Methods: interfaceMethods(typ)})
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
					file.Calls = append(file.Calls, collectCalls(d.Body, "Test", rel+"#"+d.Name.Name)...)
				} else {
					file.Functions = append(file.Functions, fn)
					nodes = append(nodes, node("Function", d.Name.Name, rel+"#"+d.Name.Name, repo, parsed.Name.Name, layer, now))
					rels = append(rels, relate("File", rel, "Function", rel+"#"+d.Name.Name, "DEFINES", repo.Root))
					file.Calls = append(file.Calls, collectCalls(d.Body, "Function", rel+"#"+d.Name.Name)...)
				}
			} else {
				recv := receiverName(d.Recv)
				file.Methods = append(file.Methods, domain.Method{Receiver: recv, Name: d.Name.Name, Path: rel, Line: line})
				nodes = append(nodes, node("Method", recv+"."+d.Name.Name, rel+"#"+recv+"."+d.Name.Name, repo, parsed.Name.Name, layer, now))
				rels = append(rels, relate("File", rel, "Method", rel+"#"+recv+"."+d.Name.Name, "DEFINES", repo.Root))
				file.Calls = append(file.Calls, collectCalls(d.Body, "Method", rel+"#"+recv+"."+d.Name.Name)...)
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

func interfaceMethods(typ *ast.InterfaceType) []string {
	var methods []string
	if typ == nil || typ.Methods == nil {
		return methods
	}
	for _, field := range typ.Methods.List {
		for _, name := range field.Names {
			methods = append(methods, name.Name)
		}
	}
	return methods
}

func collectCalls(body *ast.BlockStmt, callerKind, callerKey string) []domain.Call {
	if body == nil {
		return nil
	}
	var calls []domain.Call
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch f := call.Fun.(type) {
		case *ast.Ident:
			calls = append(calls, domain.Call{CallerKind: callerKind, CallerKey: callerKey, Callee: f.Name})
		case *ast.SelectorExpr:
			calls = append(calls, domain.Call{CallerKind: callerKind, CallerKey: callerKey, Callee: f.Sel.Name})
		}
		return true
	})
	return calls
}

func inferCodeRelationships(files []domain.IndexedFile, repoRoot string) []domain.GraphRelationship {
	symbols := map[string][]graphTarget{}
	structMethods := map[string]map[string]struct{}{}
	structKeys := map[string]string{}
	var interfaces []domain.Interface
	for _, f := range files {
		for _, fn := range f.Functions {
			symbols[fn.Name] = append(symbols[fn.Name], graphTarget{label: "Function", key: f.Path + "#" + fn.Name})
		}
		for _, m := range f.Methods {
			symbols[m.Name] = append(symbols[m.Name], graphTarget{label: "Method", key: f.Path + "#" + m.Receiver + "." + m.Name})
			symbols[m.Receiver+"."+m.Name] = append(symbols[m.Receiver+"."+m.Name], graphTarget{label: "Method", key: f.Path + "#" + m.Receiver + "." + m.Name})
			if structMethods[m.Receiver] == nil {
				structMethods[m.Receiver] = map[string]struct{}{}
			}
			structMethods[m.Receiver][m.Name] = struct{}{}
		}
		for _, s := range f.Structs {
			structKeys[s.Name] = f.Path + "#" + s.Name
		}
		interfaces = append(interfaces, f.Interfaces...)
	}
	var rels []domain.GraphRelationship
	for _, f := range files {
		for _, call := range f.Calls {
			for _, t := range symbols[call.Callee] {
				rels = append(rels, relate(call.CallerKind, call.CallerKey, t.label, t.key, "CALLS", repoRoot))
				break
			}
		}
		for _, c := range f.Contracts {
			contractKey := f.Path + "#contract:" + c.Kind + ":" + c.Name
			for _, t := range relatedCodeTargets(c, symbols) {
				rels = append(rels, relate(c.Kind, contractKey, t.label, t.key, "RELATED_TO", repoRoot))
			}
		}
		for _, test := range f.Tests {
			testKey := f.Path + "#" + test.Name
			for _, t := range testTargets(test.Name, symbols) {
				rels = append(rels, relate(t.label, t.key, "Test", testKey, "TESTED_BY", repoRoot))
			}
		}
	}
	for _, iface := range interfaces {
		required := map[string]struct{}{}
		for _, m := range iface.Methods {
			required[m] = struct{}{}
		}
		if len(required) == 0 {
			continue
		}
		for structName, methods := range structMethods {
			if implementsAll(methods, required) {
				if structKey := structKeys[structName]; structKey != "" {
					rels = append(rels, relate("Struct", structKey, "Interface", iface.Path+"#"+iface.Name, "IMPLEMENTS", repoRoot))
				}
			}
		}
	}
	return rels
}

func inferTypedRelationships(ctx context.Context, repoRoot string, files []domain.IndexedFile, graphRepo string) ([]domain.GraphRelationship, bool) {
	cfg := &packages.Config{
		Context: ctx,
		Dir:     repoRoot,
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedTypesSizes,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil || len(pkgs) == 0 {
		return nil, false
	}
	funcTargets := map[string]typedTarget{}
	var structs []struct {
		name string
		key  string
		typ  *types.Named
		path string
	}
	var ifaces []struct {
		name string
		key  string
		typ  *types.Interface
	}
	fileSet := map[string]struct{}{}
	for _, f := range files {
		fileSet[filepath.ToSlash(f.Path)] = struct{}{}
	}
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 && pkg.TypesInfo == nil {
			continue
		}
		for ident, obj := range pkg.TypesInfo.Defs {
			if obj == nil || ident == nil {
				continue
			}
			rel := relFromPackagePos(repoRoot, pkg, ident.Pos())
			if rel == "" {
				continue
			}
			if _, ok := fileSet[filepath.ToSlash(rel)]; !ok {
				continue
			}
			switch o := obj.(type) {
			case *types.Func:
				label, key := typedFuncTarget(rel, o)
				funcTargets[objectKey(o)] = typedTarget{label: label, key: key, obj: o}
			case *types.TypeName:
				named, ok := o.Type().(*types.Named)
				if !ok {
					continue
				}
				switch u := named.Underlying().(type) {
				case *types.Struct:
					_ = u
					structs = append(structs, struct {
						name string
						key  string
						typ  *types.Named
						path string
					}{name: o.Name(), key: rel + "#" + o.Name(), typ: named, path: rel})
				case *types.Interface:
					ifaces = append(ifaces, struct {
						name string
						key  string
						typ  *types.Interface
					}{name: o.Name(), key: rel + "#" + o.Name(), typ: u.Complete()})
				}
			}
		}
	}
	var rels []domain.GraphRelationship
	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok {
					return true
				}
				rel := relFromPackagePos(repoRoot, pkg, fn.Name.Pos())
				if rel == "" {
					return false
				}
				callerObj, ok := pkg.TypesInfo.Defs[fn.Name].(*types.Func)
				if !ok {
					return false
				}
				callerLabel, callerKey := typedFuncTarget(rel, callerObj)
				ast.Inspect(fn.Body, func(child ast.Node) bool {
					call, ok := child.(*ast.CallExpr)
					if !ok {
						return true
					}
					calleeObj := typedCallObject(pkg.TypesInfo, call)
					if calleeObj == nil {
						return true
					}
					if target, ok := funcTargets[objectKey(calleeObj)]; ok && target.key != callerKey {
						rels = append(rels, relate(callerLabel, callerKey, target.label, target.key, "CALLS", graphRepo))
					}
					return true
				})
				return false
			})
		}
	}
	for _, st := range structs {
		for _, iface := range ifaces {
			if types.Implements(st.typ, iface.typ) || types.Implements(types.NewPointer(st.typ), iface.typ) {
				rels = append(rels, relate("Struct", st.key, "Interface", iface.key, "IMPLEMENTS", graphRepo))
			}
		}
	}
	for _, f := range files {
		for _, c := range f.Contracts {
			contractKey := f.Path + "#contract:" + c.Kind + ":" + c.Name
			for _, target := range typedContractTargets(c, funcTargets) {
				rels = append(rels, relate(c.Kind, contractKey, target.label, target.key, "RELATED_TO", graphRepo))
			}
		}
		for _, test := range f.Tests {
			testKey := f.Path + "#" + test.Name
			for _, target := range typedTestTargets(test.Name, funcTargets) {
				rels = append(rels, relate(target.label, target.key, "Test", testKey, "TESTED_BY", graphRepo))
			}
		}
	}
	return dedupeRelationships(rels), len(rels) > 0
}

func typedFuncTarget(rel string, fn *types.Func) (string, string) {
	sig, _ := fn.Type().(*types.Signature)
	if sig != nil && sig.Recv() != nil {
		recv := typeBaseName(sig.Recv().Type())
		return "Method", rel + "#" + recv + "." + fn.Name()
	}
	return "Function", rel + "#" + fn.Name()
}

func typedCallObject(info *types.Info, call *ast.CallExpr) *types.Func {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		if fn, ok := info.Uses[f].(*types.Func); ok {
			return fn
		}
	case *ast.SelectorExpr:
		if fn, ok := info.Uses[f.Sel].(*types.Func); ok {
			return fn
		}
	case *ast.IndexExpr:
		if ident, ok := f.X.(*ast.Ident); ok {
			if fn, ok := info.Uses[ident].(*types.Func); ok {
				return fn
			}
		}
	}
	return nil
}

func relFromPackagePos(repoRoot string, pkg *packages.Package, pos token.Pos) string {
	if pkg == nil || pkg.Fset == nil || pos == token.NoPos {
		return ""
	}
	filename := pkg.Fset.Position(pos).Filename
	if filename == "" {
		return ""
	}
	rel, err := filepath.Rel(repoRoot, filename)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.ToSlash(rel)
}

func objectKey(obj types.Object) string {
	if obj == nil {
		return ""
	}
	if fn, ok := obj.(*types.Func); ok {
		sig, _ := fn.Type().(*types.Signature)
		pkgPath := ""
		if fn.Pkg() != nil {
			pkgPath = fn.Pkg().Path()
		}
		if sig != nil && sig.Recv() != nil {
			return pkgPath + "." + typeBaseName(sig.Recv().Type()) + "." + fn.Name()
		}
		return pkgPath + "." + fn.Name()
	}
	pkgPath := ""
	if obj.Pkg() != nil {
		pkgPath = obj.Pkg().Path()
	}
	return pkgPath + "." + obj.Name()
}

func typeBaseName(t types.Type) string {
	switch tt := t.(type) {
	case *types.Pointer:
		return typeBaseName(tt.Elem())
	case *types.Named:
		return tt.Obj().Name()
	default:
		s := ttString(t)
		if idx := strings.LastIndex(s, "."); idx >= 0 {
			return s[idx+1:]
		}
		return s
	}
}

func ttString(t types.Type) string {
	if t == nil {
		return ""
	}
	return types.TypeString(t, func(*types.Package) string { return "" })
}

func typedContractTargets(c domain.Contract, targets map[string]typedTarget) []typedTarget {
	parts := contractNameParts(c)
	var out []typedTarget
	for _, part := range parts {
		if len(part) < 3 {
			continue
		}
		for _, target := range targets {
			key := strings.ToLower(target.key)
			if strings.Contains(key, part) || strings.Contains(part, strings.ToLower(target.obj.Name())) {
				out = append(out, target)
			}
		}
	}
	if len(out) > 4 {
		return out[:4]
	}
	return out
}

func typedTestTargets(testName string, targets map[string]typedTarget) []typedTarget {
	lower := strings.ToLower(strings.TrimPrefix(testName, "Test"))
	var out []typedTarget
	for _, target := range targets {
		name := strings.ToLower(target.obj.Name())
		if name != "" && (strings.Contains(lower, name) || strings.Contains(name, lower)) {
			out = append(out, target)
		}
	}
	if len(out) > 3 {
		return out[:3]
	}
	return out
}

func dedupeRelationships(rels []domain.GraphRelationship) []domain.GraphRelationship {
	seen := map[string]struct{}{}
	var out []domain.GraphRelationship
	for _, rel := range rels {
		key := rel.FromLabel + "|" + rel.FromKey + "|" + rel.Type + "|" + rel.ToLabel + "|" + rel.ToKey
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, rel)
	}
	return out
}

func relatedCodeTargets(c domain.Contract, symbols map[string][]graphTarget) []graphTarget {
	var out []graphTarget
	names := contractNameParts(c)
	for _, name := range names {
		for key, targets := range symbols {
			lower := strings.ToLower(key)
			if strings.Contains(lower, name) || strings.Contains(name, lower) {
				out = append(out, targets...)
			}
		}
	}
	if len(out) > 4 {
		return out[:4]
	}
	return out
}

func contractNameParts(c domain.Contract) []string {
	name := strings.ToLower(c.Name)
	name = strings.TrimPrefix(name, "graphqltype.")
	name = strings.TrimPrefix(name, "grpcservice.")
	name = strings.TrimPrefix(name, "db.")
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '.' || r == '/' || r == '-' || r == '_' || r == ' '
	})
	for _, p := range []string{"resolver", "handler", "register", "list", "get", "create", "update", "delete"} {
		parts = append(parts, p)
	}
	return parts
}

func testTargets(testName string, symbols map[string][]graphTarget) []graphTarget {
	lower := strings.ToLower(strings.TrimPrefix(testName, "Test"))
	var out []graphTarget
	for key, targets := range symbols {
		if strings.Contains(lower, strings.ToLower(key)) || strings.Contains(strings.ToLower(key), lower) {
			out = append(out, targets...)
		}
	}
	if len(out) > 3 {
		return out[:3]
	}
	return out
}

func implementsAll(methods, required map[string]struct{}) bool {
	for req := range required {
		if _, ok := methods[req]; !ok {
			return false
		}
	}
	return true
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
