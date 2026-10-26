package instance_handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	instance_model "github.com/evolution-foundation/evolution-go/pkg/instance/model"
	instance_service "github.com/evolution-foundation/evolution-go/pkg/instance/service"
)

const (
	idDaInstanciaA = "11111111-1111-1111-1111-111111111111"
	idDaInstanciaB = "22222222-2222-2222-2222-222222222222"
)

// Só os dois métodos do advanced-settings; qualquer outro chamado estoura no
// nil embutido, o que também é falha do teste.
type servicoDeConfiguracao struct {
	instance_service.InstanceService
	lidas    []string
	gravadas []string
}

func (s *servicoDeConfiguracao) GetAdvancedSettings(instanceId string) (*instance_model.AdvancedSettings, error) {
	s.lidas = append(s.lidas, instanceId)
	return &instance_model.AdvancedSettings{RejectCall: true}, nil
}

func (s *servicoDeConfiguracao) UpdateAdvancedSettings(instanceId string, _ *instance_model.AdvancedSettings) error {
	s.gravadas = append(s.gravadas, instanceId)
	return nil
}

// Roteador com a instância A autenticada, como o middleware `Auth` deixa.
func roteadorComInstanciaA(servico *servicoDeConfiguracao) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewInstanceHandler(servico, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("instance", &instance_model.Instance{Id: idDaInstanciaA})
		c.Next()
	})
	r.GET("/instance/:instanceId/advanced-settings", h.GetAdvancedSettings)
	r.PUT("/instance/:instanceId/advanced-settings", h.UpdateAdvancedSettings)
	return r
}

func pedir(r *gin.Engine, metodo, instanceId string) *httptest.ResponseRecorder {
	corpo := strings.NewReader(`{"alwaysOnline":false,"rejectCall":true,"msgRejectCall":"","readMessages":false,"ignoreGroups":true,"ignoreStatus":false}`)
	req := httptest.NewRequest(metodo, "/instance/"+instanceId+"/advanced-settings", corpo)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Até a 0.7.5 o handler usava o instanceId do caminho e ignorava a instância
// do token: o token de uma conexão lia e mudava a configuração de outra.
func TestAdvancedSettingsDeOutraInstanciaDa403(t *testing.T) {
	for _, metodo := range []string{http.MethodGet, http.MethodPut} {
		t.Run(metodo, func(t *testing.T) {
			servico := &servicoDeConfiguracao{}
			w := pedir(roteadorComInstanciaA(servico), metodo, idDaInstanciaB)

			if w.Code != http.StatusForbidden {
				t.Errorf("status %d, esperado 403", w.Code)
			}
			if len(servico.lidas)+len(servico.gravadas) != 0 {
				t.Errorf("serviço chamado para outra instância: lidas=%v gravadas=%v", servico.lidas, servico.gravadas)
			}
		})
	}
}

func TestAdvancedSettingsDaPropriaInstanciaSegue(t *testing.T) {
	servico := &servicoDeConfiguracao{}
	r := roteadorComInstanciaA(servico)

	if w := pedir(r, http.MethodGet, idDaInstanciaA); w.Code != http.StatusOK {
		t.Errorf("GET: status %d, esperado 200", w.Code)
	}
	if w := pedir(r, http.MethodPut, idDaInstanciaA); w.Code != http.StatusOK {
		t.Errorf("PUT: status %d, esperado 200", w.Code)
	}
	if len(servico.lidas) != 1 || servico.lidas[0] != idDaInstanciaA {
		t.Errorf("leitura: %v", servico.lidas)
	}
	if len(servico.gravadas) != 1 || servico.gravadas[0] != idDaInstanciaA {
		t.Errorf("gravação: %v", servico.gravadas)
	}
}
