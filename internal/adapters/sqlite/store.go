package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NanoCycles/agent-brain/internal/domain"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func New(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Init(ctx context.Context) error {
	stmts := []string{
		`create table if not exists repositories (root text primary key, name text, go_module text, commit_sha text, indexed_at text)`,
		`create table if not exists index_runs (id integer primary key autoincrement, repo_root text, commit_sha text, started_at text, completed_at text, file_count integer, node_count integer, rel_count integer)`,
		`create table if not exists indexed_files (repo_root text, path text, package text, layer text, hash text, indexed_at text, symbols_json text, imports_json text, primary key(repo_root, path))`,
		`create table if not exists context_packs (task_id text primary key, generated_at text, md_path text, json_path text, summary text)`,
		`create table if not exists memory_proposals (task_id text primary key, path text, created_at text, applied_at text)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SaveRepository(ctx context.Context, repo domain.Repository) error {
	_, err := s.db.ExecContext(ctx, `insert into repositories(root,name,go_module,commit_sha,indexed_at) values(?,?,?,?,?)
		on conflict(root) do update set name=excluded.name, go_module=excluded.go_module, commit_sha=excluded.commit_sha, indexed_at=excluded.indexed_at`,
		repo.Root, repo.Name, repo.GoModule, repo.CommitSHA, repo.IndexedAt.Format(time.RFC3339))
	return err
}

func (s *Store) SaveIndexRun(ctx context.Context, run domain.IndexRun) error {
	_, err := s.db.ExecContext(ctx, `insert into index_runs(repo_root,commit_sha,started_at,completed_at,file_count,node_count,rel_count) values(?,?,?,?,?,?,?)`,
		run.RepoRoot, run.CommitSHA, run.StartedAt.Format(time.RFC3339), run.CompletedAt.Format(time.RFC3339), run.FileCount, run.NodeCount, run.RelCount)
	return err
}

func (s *Store) SaveIndexedFiles(ctx context.Context, repoRoot string, files []domain.IndexedFile) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, f := range files {
		symbols, _ := json.Marshal(map[string]any{"structs": f.Structs, "interfaces": f.Interfaces, "functions": f.Functions, "methods": f.Methods, "tests": f.Tests, "contracts": f.Contracts})
		imports, _ := json.Marshal(f.Imports)
		_, err := tx.ExecContext(ctx, `insert into indexed_files(repo_root,path,package,layer,hash,indexed_at,symbols_json,imports_json) values(?,?,?,?,?,?,?,?)
			on conflict(repo_root,path) do update set package=excluded.package, layer=excluded.layer, hash=excluded.hash, indexed_at=excluded.indexed_at, symbols_json=excluded.symbols_json, imports_json=excluded.imports_json`,
			repoRoot, f.Path, f.Package, f.Layer, f.Hash, f.IndexedAt.Format(time.RFC3339), string(symbols), string(imports))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) IndexedFiles(ctx context.Context, repoRoot string) ([]domain.IndexedFile, error) {
	rows, err := s.db.QueryContext(ctx, `select path, package, layer, hash, indexed_at, symbols_json, imports_json from indexed_files where repo_root=? order by path`, repoRoot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []domain.IndexedFile
	for rows.Next() {
		var f domain.IndexedFile
		var indexedAt, symbolsJSON, importsJSON string
		if err := rows.Scan(&f.Path, &f.Package, &f.Layer, &f.Hash, &indexedAt, &symbolsJSON, &importsJSON); err != nil {
			return nil, err
		}
		f.IndexedAt, _ = time.Parse(time.RFC3339, indexedAt)
		var symbols struct {
			Structs    []domain.Struct    `json:"structs"`
			Interfaces []domain.Interface `json:"interfaces"`
			Functions  []domain.Function  `json:"functions"`
			Methods    []domain.Method    `json:"methods"`
			Tests      []domain.Test      `json:"tests"`
			Contracts  []domain.Contract  `json:"contracts"`
		}
		_ = json.Unmarshal([]byte(symbolsJSON), &symbols)
		_ = json.Unmarshal([]byte(importsJSON), &f.Imports)
		f.Structs = symbols.Structs
		f.Interfaces = symbols.Interfaces
		f.Functions = symbols.Functions
		f.Methods = symbols.Methods
		f.Tests = symbols.Tests
		f.Contracts = symbols.Contracts
		files = append(files, f)
	}
	return files, rows.Err()
}

func (s *Store) LastIndexRun(ctx context.Context, repoRoot string) (*domain.IndexRun, error) {
	row := s.db.QueryRowContext(ctx, `select id, repo_root, commit_sha, started_at, completed_at, file_count, node_count, rel_count from index_runs where repo_root=? order by id desc limit 1`, repoRoot)
	var run domain.IndexRun
	var started, completed string
	err := row.Scan(&run.ID, &run.RepoRoot, &run.CommitSHA, &started, &completed, &run.FileCount, &run.NodeCount, &run.RelCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	run.StartedAt, _ = time.Parse(time.RFC3339, started)
	run.CompletedAt, _ = time.Parse(time.RFC3339, completed)
	return &run, nil
}

func (s *Store) RelevantFiles(ctx context.Context, topics []string, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `select path, package, layer, symbols_json, imports_json from indexed_files order by indexed_at desc limit 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type scored struct {
		path  string
		score int
	}
	var scoredFiles []scored
	for rows.Next() {
		var path, pkg, layer, symbols, imports string
		if err := rows.Scan(&path, &pkg, &layer, &symbols, &imports); err != nil {
			return nil, err
		}
		hay := strings.ToLower(path + " " + pkg + " " + layer + " " + symbols + " " + imports)
		score := 0
		for _, topic := range topics {
			if topic != "" && strings.Contains(hay, strings.ToLower(topic)) {
				score += 3
			}
		}
		if score > 0 {
			scoredFiles = append(scoredFiles, scored{path: path, score: score})
		}
	}
	for i := 0; i < len(scoredFiles); i++ {
		for j := i + 1; j < len(scoredFiles); j++ {
			if scoredFiles[j].score > scoredFiles[i].score {
				scoredFiles[i], scoredFiles[j] = scoredFiles[j], scoredFiles[i]
			}
		}
	}
	var out []string
	for _, sf := range scoredFiles {
		out = append(out, sf.path)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (s *Store) SaveContextPack(ctx context.Context, pack domain.ContextPack, mdPath, jsonPath string) error {
	_, err := s.db.ExecContext(ctx, `insert into context_packs(task_id, generated_at, md_path, json_path, summary) values(?,?,?,?,?)
		on conflict(task_id) do update set generated_at=excluded.generated_at, md_path=excluded.md_path, json_path=excluded.json_path, summary=excluded.summary`,
		pack.TaskID, pack.GeneratedAt.Format(time.RFC3339), mdPath, jsonPath, pack.TaskSummary)
	return err
}

func (s *Store) SaveMemoryProposal(ctx context.Context, taskID, path string) error {
	_, err := s.db.ExecContext(ctx, `insert into memory_proposals(task_id,path,created_at) values(?,?,?)
		on conflict(task_id) do update set path=excluded.path, created_at=excluded.created_at`,
		taskID, path, time.Now().Format(time.RFC3339))
	return err
}

func (s *Store) Close() error {
	return s.db.Close()
}
