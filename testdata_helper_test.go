package jsonx

import (
	"os"
	"path/filepath"
)

func readTestdataImpl(name string) ([]byte, error) {
	root, _ := os.Getwd()
	return os.ReadFile(filepath.Join(root, "testdata", name))
}
