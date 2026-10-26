package logger

import (
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func parametrosDeAcesso(path string) gin.LogFormatterParams {
	return gin.LogFormatterParams{
		TimeStamp:  time.Date(2026, 10, 26, 8, 34, 15, 0, time.Local),
		StatusCode: 401,
		Latency:    1420 * time.Microsecond,
		ClientIP:   "10.0.1.7",
		Method:     "GET",
		Path:       path,
	}
}

// O log de acesso do `gin.Default()` imprimia o caminho com a query: o
// `/ws?token=` saía inteiro na linha de acesso.
func TestFormatoDeAcessoCortaAQuery(t *testing.T) {
	linha := FormatoDeAcesso(parametrosDeAcesso("/ws?token=segredo123&instanceId=abc"))

	if strings.Contains(linha, "segredo123") || strings.Contains(linha, "?") {
		t.Errorf("query foi para o log de acesso: %q", linha)
	}
	if !strings.Contains(linha, `"/ws"`) {
		t.Errorf("caminho sumiu do log de acesso: %q", linha)
	}
}

func TestFormatoDeAcessoMantemOFormatoDoGin(t *testing.T) {
	esperado := "[GIN] 2026/10/26 - 08:34:15 | 401 |        1.42ms |        10.0.1.7 | GET      \"/instance/status\"\n"

	if linha := FormatoDeAcesso(parametrosDeAcesso("/instance/status")); linha != esperado {
		t.Errorf("formato mudou:\n veio %q\n era  %q", linha, esperado)
	}
}
