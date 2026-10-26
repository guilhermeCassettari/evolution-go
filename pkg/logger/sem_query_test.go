package logger

import "testing"

// Link assinado vai para o log só até o caminho: a query é a assinatura. Em
// 26/10/2026 a 0.7.5 imprimiu 13 links do S3 com validade de 7 dias em 11 min.
func TestSemQuery(t *testing.T) {
	casos := []struct {
		nome, url, esperado string
	}{
		{
			"link assinado do S3",
			"https://fastchat-prod-arquivos-1.s3.dualstack.sa-east-1.amazonaws.com/evolution-go-medias/AC86.jpg?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAEXEMPLO%2F20261026&X-Amz-Expires=604800&X-Amz-Signature=abc123",
			"https://fastchat-prod-arquivos-1.s3.dualstack.sa-east-1.amazonaws.com/evolution-go-medias/AC86.jpg",
		},
		{
			"avatar do CDN do WhatsApp",
			"https://pps.whatsapp.net/v/t61.24694-24/123_n.jpg?ccb=11-4&oh=01_Q5Aa&oe=6720ABCD&_nc_sid=5e03e0",
			"https://pps.whatsapp.net/v/t61.24694-24/123_n.jpg",
		},
		{
			"fragmento também sai",
			"https://exemplo.com/a/b.pdf#token=xyz",
			"https://exemplo.com/a/b.pdf",
		},
		{
			"usuário e senha na URL saem",
			"https://usuario:senha@exemplo.com/arquivo.png",
			"https://exemplo.com/arquivo.png",
		},
		{
			"URL sem query fica igual",
			"https://exemplo.com/arquivo.png",
			"https://exemplo.com/arquivo.png",
		},
		{
			"texto que não é URL não vai para o log",
			"isto não é uma URL ?segredo=1",
			urlIlegivel,
		},
		{
			"URL que o parser recusa",
			"https://exemplo.com/%zz?X-Amz-Signature=abc",
			urlIlegivel,
		},
		{
			"vazio",
			"",
			urlIlegivel,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := SemQuery(c.url); got != c.esperado {
				t.Errorf("SemQuery(%q) = %q, esperado %q", c.url, got, c.esperado)
			}
		})
	}
}
