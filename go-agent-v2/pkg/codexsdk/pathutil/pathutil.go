package pathutil

import "path/filepath"

const Separator = string(filepath.Separator)

func Abs(path string) (string, error) {
	return filepath.Abs(path)
}

func Rel(basepath string, targpath string) (string, error) {
	return filepath.Rel(basepath, targpath)
}

func Clean(path string) string {
	return filepath.Clean(path)
}

func Join(elem ...string) string {
	return filepath.Join(elem...)
}

func IsAbs(path string) bool {
	return filepath.IsAbs(path)
}

func Dir(path string) string {
	return filepath.Dir(path)
}
