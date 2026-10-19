// Package clientregistry guarda o cliente whatsmeow de cada instância em
// execução, no lugar dos mapas killChannel/clientPointer/myClientPointer, que
// eram compartilhados por todas as instâncias sem trava. Ler e gravar nesses
// mapas ao mesmo tempo dava `fatal error: concurrent map read and map write`,
// que não tem recover e derrubava o Evo Go de todas as empresas (02/10, 16/10
// e 17/10/2026).
//
// Cada StartClient cria uma Execution. Parar é fechar o canal dela: acorda
// todos que esperam (o laço de controle e a goroutine de presença) sem
// precisar de alguém lendo naquele instante, e não bloqueia. A Execution que
// termina só se tira do registro se ainda for a registrada (RemoveIf), então
// uma execução antiga que termina tarde não apaga a nova.
package clientregistry

import (
	"sync"

	"go.mau.fi/whatsmeow"
)

// StopReason diz por que uma execução parou. O laço de controle usa o motivo
// para decidir o que publicar (só logout vira evento LoggedOut).
type StopReason string

const (
	// StopLogout: o WhatsApp deslogou a instância (events.LoggedOut).
	StopLogout StopReason = "logout"
	// StopQRTimeout: os QR acabaram sem pareamento.
	StopQRTimeout StopReason = "qr_timeout"
	// StopReconnect: o socket caiu e quem parou vai subir uma execução nova.
	StopReconnect StopReason = "reconnect"
	// StopRemove: parada pedida pela API (logout, delete, disconnect,
	// forcereconnect).
	StopRemove StopReason = "remove"
	// StopReplaced: outra execução da mesma instância foi registrada.
	StopReplaced StopReason = "replaced"
)

// Execution é uma execução de StartClient: um cliente whatsmeow, o MyClient
// dele e o canal que a encerra.
type Execution struct {
	instanceID string
	client     *whatsmeow.Client
	myClient   any
	done       chan struct{}
	once       sync.Once
	reason     StopReason
}

// NewExecution cria a execução de uma instância. myClient é o *MyClient do
// service whatsmeow; fica como `any` para este pacote não importar o service.
func NewExecution(instanceID string, client *whatsmeow.Client, myClient any) *Execution {
	return &Execution{
		instanceID: instanceID,
		client:     client,
		myClient:   myClient,
		done:       make(chan struct{}),
	}
}

func (e *Execution) InstanceID() string        { return e.instanceID }
func (e *Execution) Client() *whatsmeow.Client { return e.client }
func (e *Execution) MyClient() any             { return e.myClient }
func (e *Execution) Done() <-chan struct{}     { return e.done }

// Stop fecha o canal da execução. Devolve true só na primeira chamada; as
// seguintes não fazem nada e não mudam o motivo.
func (e *Execution) Stop(reason StopReason) bool {
	first := false
	e.once.Do(func() {
		e.reason = reason
		close(e.done)
		first = true
	})
	return first
}

// Stopped diz se a execução já recebeu ordem de parar.
func (e *Execution) Stopped() bool {
	select {
	case <-e.done:
		return true
	default:
		return false
	}
}

// Reason é o motivo da primeira parada. Só é confiável depois de Done fechar,
// e o fechamento do canal garante que quem leu Done enxerga o valor.
func (e *Execution) Reason() StopReason {
	if !e.Stopped() {
		return ""
	}
	return e.reason
}

// Registry é o registro das execuções vivas, uma por instância.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*Execution
}

func New() *Registry {
	return &Registry{entries: make(map[string]*Execution)}
}

// Register põe a execução como a atual da instância e para a anterior, se
// houver, com StopReplaced. Duas execuções da mesma instância ao mesmo tempo
// eram como sobravam sockets do WhatsApp abertos.
func (r *Registry) Register(e *Execution) (previous *Execution) {
	r.mu.Lock()
	previous = r.entries[e.instanceID]
	r.entries[e.instanceID] = e
	r.mu.Unlock()

	if previous != nil && previous != e {
		previous.Stop(StopReplaced)
	}
	return previous
}

// RemoveIf tira a execução do registro só se ela ainda for a atual.
func (r *Registry) RemoveIf(e *Execution) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries[e.instanceID] != e {
		return false
	}
	delete(r.entries, e.instanceID)
	return true
}

func (r *Registry) IsCurrent(e *Execution) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.entries[e.instanceID] == e
}

func (r *Registry) Current(instanceID string) *Execution {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.entries[instanceID]
}

// Client devolve o cliente da execução atual, ou nil.
func (r *Registry) Client(instanceID string) *whatsmeow.Client {
	if e := r.Current(instanceID); e != nil {
		return e.client
	}
	return nil
}

// MyClient devolve o MyClient da execução atual, ou nil.
func (r *Registry) MyClient(instanceID string) any {
	if e := r.Current(instanceID); e != nil {
		return e.myClient
	}
	return nil
}

// StopCurrent para a execução atual da instância. É para as rotas da API, que
// agem sobre a instância. Handler de evento do whatsmeow para a própria
// execução (Execution.Stop), nunca a atual: o evento atrasado de um cliente
// antigo não pode derrubar o cliente novo.
//
// Só sinaliza: quem desconecta e tira do registro é o laço da execução.
func (r *Registry) StopCurrent(instanceID string, reason StopReason) bool {
	e := r.Current(instanceID)
	if e == nil {
		return false
	}
	return e.Stop(reason)
}

// Count é o número de execuções registradas.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}
