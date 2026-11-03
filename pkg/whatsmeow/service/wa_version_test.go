package whatsmeow_service

import (
	"sync"
	"testing"

	"go.mau.fi/whatsmeow/store"
)

// O que vai na conexão é store.GetWAVersion(), não store.DeviceProps.Version.
// Em 02/11/2026 o WhatsApp passou a recusar (405) a versão fixa da biblioteca e
// todas as instâncias caíram, embora o log dissesse que usava a versão da web.
func TestAplicarVersaoWhatsAppMudaOQueVaiNaConexao(t *testing.T) {
	original := store.GetWAVersion()
	t.Cleanup(func() { store.SetWAVersion(original) })

	aplicarVersaoWhatsApp(clientVersion{Major: 2, Minor: 3000, Patch: 1052846864})

	if got := store.GetWAVersion(); got != (store.WAVersionContainer{2, 3000, 1052846864}) {
		t.Fatalf("versão da conexão = %s", got.String())
	}
}

func TestAplicarVersaoWhatsAppIgnoraVersaoZerada(t *testing.T) {
	original := store.GetWAVersion()
	t.Cleanup(func() { store.SetWAVersion(original) })

	aplicarVersaoWhatsApp(clientVersion{})

	if got := store.GetWAVersion(); got != original {
		t.Fatalf("versão zerada trocou a da conexão para %s", got.String())
	}
}

// Instâncias sobem em paralelo, e a versão é global na biblioteca.
func TestAplicarVersaoWhatsAppEmParalelo(t *testing.T) {
	original := store.GetWAVersion()
	t.Cleanup(func() { store.SetWAVersion(original) })

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			aplicarVersaoWhatsApp(clientVersion{Major: 2, Minor: 3000, Patch: 1052846864 + n%2})
		}(i)
	}
	wg.Wait()
}
