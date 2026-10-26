package logger

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Até a 0.7.5 o link assinado do S3 de toda mídia recebida ia para a saída por
// fmt.Println, e as URLs de mídia, avatar e webhook iam inteiras para o log —
// com a assinatura na query. URL só entra no log por SemQuery.
var (
	printDeLinkAssinado = regexp.MustCompile(`\bfmt\.Print[a-z]*\(.*(?i:presign)`)
	logComURLInteira    = regexp.MustCompile(`\bLog[A-Za-z]*\(.*,\s*(\w+\.)*[a-z]*(URL|Url|url)\s*[,)]`)
)

func TestNenhumLogImprimeURLComAssinatura(t *testing.T) {
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
				if printDeLinkAssinado.MatchString(line) {
					t.Errorf("%s:%d imprime link assinado: %s", path, i+1, strings.TrimSpace(line))
				}
				if logComURLInteira.MatchString(line) {
					t.Errorf("%s:%d loga URL sem SemQuery: %s", path, i+1, strings.TrimSpace(line))
				}
			}
			return nil
		})
	}
}

// O próprio guarda precisa pegar o que existia na 0.7.5 e deixar passar a
// forma certa; sem isso, uma regex quebrada fica verde para sempre.
func TestGuardaDeURLReconheceOsCasos(t *testing.T) {
	pega := []string{
		`fmt.Println(presignedURL.String())`,
		`l.LogInfo("[%s] S3/Minio upload successful - ID: %s, URL: %s", id, evt.Info.ID, mediaURL)`,
		`l.LogInfo("[%s] Iniciando download da URL: %s", instance.Id, data.Url)`,
		`l.LogInfo("[%s] Got avatar %s", instance.Id, pic.URL)`,
		`l.LogInfo("[%s] webhook sent - url: %s, status: %d", userID, url, statusCode)`,
	}
	deixa := []string{
		`l.LogInfo("[%s] Iniciando download da URL: %s", instance.Id, logger.SemQuery(data.Url))`,
		`l.LogInfo("[%s] Got avatar %s", instance.Id, logger.SemQuery(pic.URL))`,
		`logger.LogInfo("[CONFIG] AMQP URL validation successful: %s://%s", parsedURL.Scheme, parsedURL.Host)`,
		`return presignedURL.String(), nil`,
	}
	for _, l := range pega {
		if !printDeLinkAssinado.MatchString(l) && !logComURLInteira.MatchString(l) {
			t.Errorf("guarda deixou passar: %s", l)
		}
	}
	for _, l := range deixa {
		if printDeLinkAssinado.MatchString(l) || logComURLInteira.MatchString(l) {
			t.Errorf("guarda acusou a forma certa: %s", l)
		}
	}
}
