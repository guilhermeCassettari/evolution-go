// Package safehttp baixa URL que chega numa requisição. Antes, cada ponto
// fazia http.Get direto: quem tinha um token de instância podia pedir
// http://grafana_loki:3100/... e receber a resposta como arquivo no WhatsApp,
// porque o Evo Go fica na mesma rede dos serviços internos. Também não havia
// limite de tamanho nem de tempo.
//
// A checagem do destino é feita no Dialer.Control, que recebe o IP já
// resolvido na hora de abrir o socket. Assim ela vale para IP direto, para nome
// que resolve para dentro e para cada salto de redirecionamento, que abre
// conexão nova.
package safehttp

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

const (
	// MaxBytes é o maior corpo aceito. O FastChat manda mídia de até 180 MiB.
	MaxBytes = 190 << 20
	// Timeout vale para a requisição inteira, leitura do corpo incluída. O
	// backend do FastChat espera no máximo 300 s pelo /send/media.
	Timeout = 5 * time.Minute
)

var (
	ErrBlockedAddress = errors.New("safehttp: endereço de destino não permitido")
	ErrTooLarge       = errors.New("safehttp: resposta maior que o limite")
)

// Faixas que não são internet pública e que netip não classifica sozinho.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"), // reservado, inclui broadcast
}

func isPublic(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() || addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || addr.IsMulticast() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

type client struct {
	http     *http.Client
	maxBytes int64
}

func newClient(maxBytes int64, timeout time.Duration, allow func(netip.AddrPort) bool) *client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !allow(ap) {
				return fmt.Errorf("%w: %s", ErrBlockedAddress, address)
			}
			return nil
		},
	}
	transport := &http.Transport{
		// Sem proxy: com ele, a conexão iria ao proxy e o Control olharia o IP errado.
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		ForceAttemptHTTP2:     true,
	}
	return &client{
		http:     &http.Client{Transport: transport, Timeout: timeout},
		maxBytes: maxBytes,
	}
}

var defaultClient = newClient(MaxBytes, Timeout, func(ap netip.AddrPort) bool { return isPublic(ap.Addr()) })

// Get troca o http.Get nos downloads pedidos por requisição. O corpo devolvido
// dá ErrTooLarge ao passar de MaxBytes, em vez de cortar o arquivo.
func Get(rawURL string) (*http.Response, error) {
	return defaultClient.Get(rawURL)
}

func (c *client) Get(rawURL string) (*http.Response, error) {
	resp, err := c.http.Get(rawURL)
	if err != nil {
		return nil, err
	}
	if resp.ContentLength > c.maxBytes {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %d bytes", ErrTooLarge, resp.ContentLength)
	}
	resp.Body = &limitedBody{ReadCloser: resp.Body, left: c.maxBytes}
	return resp, nil
}

type limitedBody struct {
	io.ReadCloser
	left int64
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		// No limite: só é erro se o servidor ainda tiver o que mandar.
		var one [1]byte
		n, err := b.ReadCloser.Read(one[:])
		if n > 0 {
			return 0, ErrTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.ReadCloser.Read(p)
	b.left -= int64(n)
	return n, err
}
