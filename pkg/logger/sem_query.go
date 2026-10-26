package logger

import "net/url"

// urlIlegivel substitui no log a URL que não dá para interpretar: o texto
// original pode ser exatamente onde está o segredo.
const urlIlegivel = "<url ilegível>"

// SemQuery devolve a URL só com esquema, host e caminho, para ir ao log. A
// query e o fragmento carregam a assinatura de link do S3 (X-Amz-Signature) e
// do CDN do WhatsApp (oh/oe), e usuário e senha também saem.
func SemQuery(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return urlIlegivel
	}

	limpa := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawPath: u.RawPath}
	return limpa.String()
}
