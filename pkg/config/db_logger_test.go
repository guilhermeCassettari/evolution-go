package config

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Até a 0.7.5 o GORM rodava com o logger padrão: toda chave errada que chegava
// ao middleware virava "record not found" no log com `WHERE token = '<chave>'`.
// Em 26/10/2026 era a chave mestra, mandada pelo backend ao advanced-settings.
const chaveDeTeste = "4f1c0ffee0000000000000000000000000000000000000000000000000000abc"

type instanciaDeTeste struct {
	ID    string
	Token string
}

func (instanciaDeTeste) TableName() string { return "instances" }

func abrirComDbLogger(t *testing.T, saida *bytes.Buffer) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: dbLogger(saida)})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	return db, mock
}

func TestDbLoggerNaoRegistraRecordNotFound(t *testing.T) {
	var saida bytes.Buffer
	db, mock := abrirComDbLogger(t, &saida)
	mock.ExpectQuery(`SELECT .* FROM "instances"`).WillReturnRows(sqlmock.NewRows([]string{"id", "token"}))

	var inst instanciaDeTeste
	err := db.Where("token = ?", chaveDeTeste).First(&inst).Error

	if err != gorm.ErrRecordNotFound {
		t.Fatalf("esperado ErrRecordNotFound, veio %v", err)
	}
	if saida.Len() != 0 {
		t.Errorf("record not found foi para o log: %q", saida.String())
	}
}

func TestDbLoggerConsultaLentaSaiSemValores(t *testing.T) {
	var saida bytes.Buffer
	db, mock := abrirComDbLogger(t, &saida)
	mock.ExpectQuery(`SELECT .* FROM "instances"`).
		WillDelayFor(250 * time.Millisecond).
		WillReturnRows(sqlmock.NewRows([]string{"id", "token"}).AddRow("1", chaveDeTeste))

	var inst instanciaDeTeste
	if err := db.Where("token = ?", chaveDeTeste).First(&inst).Error; err != nil {
		t.Fatalf("consulta: %v", err)
	}

	log := saida.String()
	if !strings.Contains(log, "SLOW SQL") {
		t.Fatalf("consulta lenta deveria continuar no log, veio %q", log)
	}
	if strings.Contains(log, chaveDeTeste) {
		t.Errorf("valor da consulta foi para o log: %q", log)
	}
}

func TestDbLoggerErroSaiSemValores(t *testing.T) {
	var saida bytes.Buffer
	db, mock := abrirComDbLogger(t, &saida)
	mock.ExpectQuery(`SELECT .* FROM "instances"`).WillReturnError(sqlmock.ErrCancelled)

	var inst instanciaDeTeste
	db.Where("token = ?", chaveDeTeste).First(&inst)

	log := saida.String()
	if !strings.Contains(log, "canceling query") {
		t.Fatalf("erro de verdade deveria continuar no log, veio %q", log)
	}
	if strings.Contains(log, chaveDeTeste) {
		t.Errorf("valor da consulta foi para o log: %q", log)
	}
}
