package ports

type FileSystem interface {
	Exists(path string) bool
	EnsureDir(path string) error
	WriteFileIfMissing(path string, data []byte, perm uint32) error
	ReadFile(path string) ([]byte, error)
}
