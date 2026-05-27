package importlib_resources

import (
	"os"
	"path/filepath"
	"runtime"
)

func Files(module string) string {
	_, filename, _, _ := runtime.Caller(1)
	return filepath.Join(filepath.Dir(filename), "..", "..", module)
}

func ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
