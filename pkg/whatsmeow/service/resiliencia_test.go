package whatsmeow_service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/evolution-foundation/evolution-go/pkg/clientregistry"
	"github.com/evolution-foundation/evolution-go/pkg/config"
	instance_model "github.com/evolution-foundation/evolution-go/pkg/instance/model"
	instance_repository "github.com/evolution-foundation/evolution-go/pkg/instance/repository"
	"github.com/evolution-foundation/evolution-go/pkg/logger"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
)

// Aponta a busca de versão para um servidor de teste e restaura tudo no fim.
func prepararBuscaDeVersao(t *testing.T, handler http.HandlerFunc, timeout time.Duration) *atomic.Int32 {
	t.Helper()
	chamadas := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chamadas.Add(1)
		handler(w, r)
	}))

	urlOriginal, httpOriginal := webVersionURL, webVersionHTTP
	versaoOriginal := store.GetWAVersion()
	limparCacheDeVersaoWeb()
	webVersionURL = srv.URL
	webVersionHTTP = &http.Client{Timeout: timeout}

	t.Cleanup(func() {
		srv.Close()
		webVersionURL, webVersionHTTP = urlOriginal, httpOriginal
		limparCacheDeVersaoWeb()
		store.SetWAVersion(versaoOriginal)
	})
	return chamadas
}

func responderRevisao(revisao string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		// O formato escapado é o que o sw.js publica hoje.
		_, _ = w.Write([]byte(`self.x={\"client_revision\":` + revisao + `,\"y\":1}`))
	}
}

func TestBuscaDeVersaoLeARevisaoPublicada(t *testing.T) {
	prepararBuscaDeVersao(t, responderRevisao("1052846864"), time.Second)

	versao, err := fetchWhatsAppWebVersion()
	if err != nil {
		t.Fatal(err)
	}
	if *versao != (clientVersion{Major: 2, Minor: 3000, Patch: 1052846864}) {
		t.Fatalf("versão = %+v", *versao)
	}
}

// O http.Get sem timeout, com o mutex preso, deixaria toda instância esperando
// se o WhatsApp Web pendurasse.
func TestBuscaDeVersaoNaoPenduraQuandoAWebNaoResponde(t *testing.T) {
	prepararBuscaDeVersao(t, func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, 50*time.Millisecond)

	inicio := time.Now()
	if _, err := fetchWhatsAppWebVersion(); err == nil {
		t.Fatal("esperava erro de timeout")
	}
	if decorrido := time.Since(inicio); decorrido > 2*time.Second {
		t.Fatalf("demorou %s", decorrido)
	}
}

// Com a web fora, as instâncias sobem em fila: cada uma esperaria o timeout inteiro.
func TestBuscaDeVersaoNaoInsisteLogoDepoisDeFalhar(t *testing.T) {
	chamadas := prepararBuscaDeVersao(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}, time.Second)

	for i := 0; i < 3; i++ {
		if _, err := fetchWhatsAppWebVersion(); err == nil {
			t.Fatal("esperava erro")
		}
	}
	if got := chamadas.Load(); got != 1 {
		t.Fatalf("foi à web %d vezes", got)
	}
}

func TestBuscaDeVersaoUsaOCacheQuandoNaoForcada(t *testing.T) {
	chamadas := prepararBuscaDeVersao(t, responderRevisao("1052846864"), time.Second)

	for i := 0; i < 3; i++ {
		if _, err := fetchWhatsAppWebVersion(); err != nil {
			t.Fatal(err)
		}
	}
	if got := chamadas.Load(); got != 1 {
		t.Fatalf("foi à web %d vezes", got)
	}
}

// Todas as instâncias recebem o 405 juntas: só a primeira vai à web.
func TestBuscaForcadaIgnoraOCacheMasNaoMaisDeUmaVezPorMinuto(t *testing.T) {
	chamadas := prepararBuscaDeVersao(t, responderRevisao("1052846864"), time.Second)

	if _, err := fetchWhatsAppWebVersion(); err != nil {
		t.Fatal(err)
	}
	cachedWebVersionAt = time.Now().Add(-2 * webVersionIntervaloMinimo)

	for i := 0; i < 3; i++ {
		if _, err := buscarVersaoWeb(true); err != nil {
			t.Fatal(err)
		}
	}
	if got := chamadas.Load(); got != 2 {
		t.Fatalf("foi à web %d vezes", got)
	}
}

func TestDepoisDoClientOutdatedAplicaAVersaoNova(t *testing.T) {
	prepararBuscaDeVersao(t, responderRevisao("1052999999"), time.Second)
	store.SetWAVersion(store.WAVersionContainer{2, 3000, 1042386815})

	atual, err := atualizarVersaoAposClientOutdated(&config.Config{})
	if err != nil {
		t.Fatal(err)
	}

	esperada := store.WAVersionContainer{2, 3000, 1052999999}
	if atual != esperada || store.GetWAVersion() != esperada {
		t.Fatalf("atual=%s na conexão=%s", atual.String(), store.GetWAVersion().String())
	}
}

// Versão fixada por configuração é escolha de quem configurou.
func TestDepoisDoClientOutdatedNaoTrocaVersaoFixadaPorConfiguracao(t *testing.T) {
	chamadas := prepararBuscaDeVersao(t, responderRevisao("1052999999"), time.Second)
	fixada := store.WAVersionContainer{2, 3000, 1042386815}
	store.SetWAVersion(fixada)

	atual, err := atualizarVersaoAposClientOutdated(&config.Config{
		WhatsappVersionMajor: 2, WhatsappVersionMinor: 3000, WhatsappVersionPatch: 1042386815,
	})
	if err != nil {
		t.Fatal(err)
	}
	if atual != fixada || chamadas.Load() != 0 {
		t.Fatalf("atual=%s chamadas=%d", atual.String(), chamadas.Load())
	}
}

// Só reconecta se houver versão nova: repetir a recusada só insiste no WhatsApp.
func TestReconectaDepoisDoClientOutdatedSoComVersaoNova(t *testing.T) {
	usada := store.WAVersionContainer{2, 3000, 1042386815}

	if !deveReconectarAposClientOutdated(usada, store.WAVersionContainer{2, 3000, 1052846864}) {
		t.Fatal("versão nova deveria reconectar")
	}
	if deveReconectarAposClientOutdated(usada, usada) {
		t.Fatal("mesma versão não deveria reconectar")
	}
}

func TestConfigurarClienteWhatsApp(t *testing.T) {
	client := &whatsmeow.Client{}

	configurarClienteWhatsApp(client)

	if client.EnableAutoReconnect {
		t.Error("o religamento é do Evo Go")
	}
	if !client.AutoTrustIdentity {
		t.Error("AutoTrustIdentity deveria seguir ligado")
	}
	if !client.AutomaticMessageRerequestFromPhone {
		t.Error("deveria pedir ao celular as mensagens que não chegam")
	}
	if client.UseRetryMessageStore {
		t.Error("o store de reenvio grava as mensagens enviadas em texto puro")
	}
	// A biblioteca chama RefreshCAT sem checar nulo na falha 413/414 do WhatsApp.
	if client.RefreshCAT == nil {
		t.Fatal("RefreshCAT nulo vira panic e derruba todas as instâncias")
	}
	if err := client.RefreshCAT(context.Background()); err == nil {
		t.Error("RefreshCAT deveria devolver erro: só existe no Messenger")
	}
}

type repositorioFalso struct {
	instance_repository.InstanceRepository
	motivos []string
}

func (r *repositorioFalso) UpdateConnected(_ string, status bool, motivo string) error {
	if status {
		return errors.New("não deveria marcar como conectada")
	}
	r.motivos = append(r.motivos, motivo)
	return nil
}

type servicoFalso struct {
	WhatsmeowService
	reconexoes int
}

func (s *servicoFalso) ReconnectClient(string) error {
	s.reconexoes++
	return nil
}

func clienteComExecucao(t *testing.T, usada store.WAVersionContainer) (*MyClient, *repositorioFalso, *servicoFalso) {
	t.Helper()
	// O logger grava uma pasta por instância no diretório atual.
	t.Chdir(t.TempDir())
	repositorio := &repositorioFalso{}
	servico := &servicoFalso{}
	registro := clientregistry.New()
	execucao := clientregistry.NewExecution("instancia", nil, nil)
	registro.Register(execucao)
	return &MyClient{
		versaoNaConexao:    usada,
		service:            servico,
		userID:             "instancia",
		Instance:           &instance_model.Instance{Id: "instancia"},
		instanceRepository: repositorio,
		config:             &config.Config{},
		loggerWrapper:      logger.NewLoggerManager(&config.Config{}),
		registry:           registro,
		execution:          execucao,
	}, repositorio, servico
}

func TestClientOutdatedComVersaoNovaReconectaEExplicaOMotivo(t *testing.T) {
	prepararBuscaDeVersao(t, responderRevisao("1052999999"), time.Second)
	mycli, repositorio, servico := clienteComExecucao(t, store.WAVersionContainer{2, 3000, 1042386815})

	mycli.tratarClientOutdated()

	if servico.reconexoes != 1 {
		t.Fatalf("reconexões = %d", servico.reconexoes)
	}
	if len(repositorio.motivos) != 1 || repositorio.motivos[0] != motivoClientOutdated {
		t.Fatalf("motivos = %v", repositorio.motivos)
	}
	if mycli.Instance.DisconnectReason != motivoClientOutdated {
		t.Fatalf("motivo da instância = %q", mycli.Instance.DisconnectReason)
	}
}

// A nova conexão pode receber 405 de novo: sem versão nova, para. É o que impede o laço.
func TestClientOutdatedSemVersaoNovaNaoReconecta(t *testing.T) {
	prepararBuscaDeVersao(t, responderRevisao("1052999999"), time.Second)
	mycli, repositorio, servico := clienteComExecucao(t, store.WAVersionContainer{2, 3000, 1052999999})

	mycli.tratarClientOutdated()

	if servico.reconexoes != 0 {
		t.Fatalf("reconexões = %d", servico.reconexoes)
	}
	if len(repositorio.motivos) != 1 {
		t.Fatalf("motivos = %v", repositorio.motivos)
	}
}

func TestClientOutdatedComAWebForaNaoReconecta(t *testing.T) {
	prepararBuscaDeVersao(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}, time.Second)
	mycli, repositorio, servico := clienteComExecucao(t, store.WAVersionContainer{2, 3000, 1042386815})

	mycli.tratarClientOutdated()

	if servico.reconexoes != 0 {
		t.Fatalf("reconexões = %d", servico.reconexoes)
	}
	if len(repositorio.motivos) != 1 || repositorio.motivos[0] != motivoClientOutdated {
		t.Fatalf("motivos = %v", repositorio.motivos)
	}
}

// Cliente substituído não mexe na execução nova.
func TestClientOutdatedDeExecucaoAntigaNaoReconecta(t *testing.T) {
	prepararBuscaDeVersao(t, responderRevisao("1052999999"), time.Second)
	mycli, _, servico := clienteComExecucao(t, store.WAVersionContainer{2, 3000, 1042386815})
	mycli.registry.Register(clientregistry.NewExecution("instancia", nil, nil))

	mycli.tratarClientOutdated()

	if servico.reconexoes != 0 {
		t.Fatalf("reconexões = %d", servico.reconexoes)
	}
}
