package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type LocalFS struct{}

func (LocalFS) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (LocalFS) EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func (LocalFS) WriteFileIfMissing(path string, data []byte, perm uint32) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, os.FileMode(perm))
}

func (LocalFS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

var forbiddenNames = map[string]struct{}{
	".git": {}, "vendor": {}, "node_modules": {}, "dist": {}, "build": {}, "target": {},
	"bin": {}, "coverage": {}, "credentials": {}, "secrets": {}, "kubeconfig": {},
	"id_rsa": {}, "id_ed25519": {},
}

var forbiddenExts = []string{".pem", ".key"}

func IsForbiddenPath(path string) bool {
	clean := filepath.Clean(path)
	parts := strings.FieldsFunc(clean, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	for _, part := range parts {
		lower := strings.ToLower(part)
		if lower == ".env" || strings.HasPrefix(lower, ".env.") {
			return true
		}
		if _, ok := forbiddenNames[lower]; ok {
			return true
		}
		for _, ext := range forbiddenExts {
			if strings.HasSuffix(lower, ext) {
				return true
			}
		}
	}
	return false
}

func IsIgnoredDir(name string) bool {
	_, ok := forbiddenNames[strings.ToLower(name)]
	return ok
}
