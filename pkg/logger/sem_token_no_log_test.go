package logger

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// O token de instância é a credencial da API. Até a 0.7.4 ele ia para o log em
// 6 mensagens de limpeza de cache, o /ws imprimia a chave tentada e o
// "Attempting to update instance in DB" despejava a instância inteira com %+v.
var logComToken = regexp.MustCompile(`\bLog[A-Za-z]*\(.*(\.Token\b|,\s*token\s*\)|%\+?v.*,\s*(\w+\.)?[Ii]nstance\s*\))`)

func TestNenhumLogImprimeToken(t *testing.T) {
	for _, dir := range []string{"../../pkg", "../../cmd"} {
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(src), "\n") {
				if logComToken.MatchString(line) {
					t.Errorf("%s:%d imprime token: %s", path, i+1, strings.TrimSpace(line))
				}
			}
			return nil
		})
	}
}
