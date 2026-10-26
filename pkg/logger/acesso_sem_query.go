package logger

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// FormatoDeAcesso é o log de acesso do GIN sem a query string. O formatador
// padrão imprime `param.Path` com a query, e o `/ws` recebe a chave por ela.
// O resto da linha segue o formato do `gin.Default()` sem cores.
func FormatoDeAcesso(param gin.LogFormatterParams) string {
	if param.Latency > time.Minute {
		param.Latency = param.Latency.Truncate(time.Second)
	}
	caminho, _, _ := strings.Cut(param.Path, "?")
	return fmt.Sprintf("[GIN] %v | %3d | %13v | %15s | %-7s  %#v\n%s",
		param.TimeStamp.Format("2006/01/02 - 15:04:05"),
		param.StatusCode,
		param.Latency,
		param.ClientIP,
		param.Method,
		caminho,
		param.ErrorMessage,
	)
}
