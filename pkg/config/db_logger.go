package config

import (
	"io"
	"log"
	"time"

	gormlogger "gorm.io/gorm/logger"
)

// dbLogger é o logger do GORM sem os valores das consultas. O padrão
// (`&gorm.Config{}`) registrava cada "record not found" com o SQL preenchido, e
// o middleware procura instância pelo token: toda chave errada — inclusive a
// chave mestra — ia para o log em claro. Consulta lenta e erro de verdade
// continuam registrados, com `$1` no lugar do valor.
func dbLogger(w io.Writer) gormlogger.Interface {
	return gormlogger.New(log.New(w, "\r\n", log.LstdFlags), gormlogger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  gormlogger.Warn,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
		Colorful:                  false,
	})
}
