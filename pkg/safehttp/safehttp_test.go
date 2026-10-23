package safehttp

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsPublic(t *testing.T) {
	bloqueados := []string{
		"127.0.0.1", "127.8.8.8", "::1",
		"10.0.1.7", "172.18.0.1", "192.168.1.1", "fd00::1",
		"100.66.21.90", "100.64.0.1", // CGNAT
		"169.254.169.254", "fe80::1",
		"0.0.0.0", "0.1.2.3", "::",
		"224.0.0.1", "ff02::1", "255.255.255.255", "240.0.0.1",
		"192.0.0.1", "198.18.0.1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1",
	}
	for _, s := range bloqueados {
		if isPublic(netip.MustParseAddr(s)) {
			t.Errorf("%s deveria ser bloqueado", s)
		}
	}

	permitidos := []string{"8.8.8.8", "52.95.164.10", "45.4.120.10", "100.128.0.1", "2600:1f18::1", "::ffff:8.8.8.8"}
	for _, s := range permitidos {
		if !isPublic(netip.MustParseAddr(s)) {
			t.Errorf("%s deveria ser permitido", s)
		}
	}
}

// O httptest escuta em 127.0.0.1, então o cliente de produção recusa todos.
func TestGetRecusaIPInterno(t *testing.T) {
	var chamado atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chamado.Store(true)
	}))
	defer srv.Close()

	_, err := Get(srv.URL)
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("esperava ErrBlockedAddress, veio %v", err)
	}
	if chamado.Load() {
		t.Fatal("a requisição chegou ao servidor interno")
	}
}

func TestGetRecusaNomeQueResolveParaLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	_, err := Get("http://localhost:" + u.Port() + "/")
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("esperava ErrBlockedAddress, veio %v", err)
	}
}

func TestGetRecusaRedirecionamentoParaDentro(t *testing.T) {
	var internoChamado atomic.Bool
	interno := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		internoChamado.Store(true)
	}))
	defer interno.Close()

	publico := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, interno.URL+"/segredo", http.StatusFound)
	}))
	defer publico.Close()

	// Só a porta do "público" passa; o salto para o "interno" tem de ser recusado.
	c := newClient(MaxBytes, time.Minute, soPorta(t, publico.URL))
	_, err := c.Get(publico.URL)
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("esperava ErrBlockedAddress no redirecionamento, veio %v", err)
	}
	if internoChamado.Load() {
		t.Fatal("o redirecionamento chegou ao servidor interno")
	}
}

func TestGetPermitidoDevolveOCorpo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "conteudo")
	}))
	defer srv.Close()

	c := newClient(MaxBytes, time.Minute, soPorta(t, srv.URL))
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "conteudo" {
		t.Fatalf("corpo %q, erro %v", body, err)
	}
}

func TestCorpoNoLimiteExatoPassa(t *testing.T) {
	srv := servidorDeBytes(10, false)
	defer srv.Close()

	c := newClient(10, time.Minute, soPorta(t, srv.URL))
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || len(body) != 10 {
		t.Fatalf("leu %d bytes, erro %v", len(body), err)
	}
}

func TestContentLengthAcimaDoLimiteFalhaAntesDeLer(t *testing.T) {
	srv := servidorDeBytes(11, false)
	defer srv.Close()

	c := newClient(10, time.Minute, soPorta(t, srv.URL))
	_, err := c.Get(srv.URL)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("esperava ErrTooLarge, veio %v", err)
	}
}

// Sem Content-Length (chunked), o erro vem na leitura e nunca é arquivo cortado.
func TestCorpoChunkedAcimaDoLimiteFalhaNaLeitura(t *testing.T) {
	srv := servidorDeBytes(11, true)
	defer srv.Close()

	c := newClient(10, time.Minute, soPorta(t, srv.URL))
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("esperava ErrTooLarge na leitura, veio %v", err)
	}
}

func TestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	c := newClient(MaxBytes, 100*time.Millisecond, soPorta(t, srv.URL))
	inicio := time.Now()
	_, err := c.Get(srv.URL)
	var ue *url.Error
	if !errors.As(err, &ue) || !ue.Timeout() {
		t.Fatalf("esperava timeout, veio %v", err)
	}
	if time.Since(inicio) > time.Second {
		t.Fatalf("demorou %v para desistir", time.Since(inicio))
	}
}

// soPorta libera só o endereço do servidor de teste, que escuta em loopback.
func soPorta(t *testing.T, raw string) func(netip.AddrPort) bool {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	porta, _ := strconv.Atoi(u.Port())
	return func(ap netip.AddrPort) bool { return int(ap.Port()) == porta }
}

func servidorDeBytes(n int, chunked bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corpo := bytes.Repeat([]byte("a"), n)
		if chunked {
			// Flush antes do fim força Transfer-Encoding: chunked.
			w.Write(corpo[:1])
			w.(http.Flusher).Flush()
			w.Write(corpo[1:])
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(n))
		w.Write(corpo)
	}))
}
