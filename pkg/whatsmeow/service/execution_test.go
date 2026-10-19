package whatsmeow_service

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/evolution-foundation/evolution-go/pkg/clientregistry"
)

func TestStopEffects(t *testing.T) {
	cases := []struct {
		reason           clientregistry.StopReason
		updateDB         bool
		publishLoggedOut bool
	}{
		// A new execution takes over: writing connected=false or announcing a
		// logout after it connects would leave the DB and FastChat wrong.
		{clientregistry.StopReconnect, false, false},
		{clientregistry.StopReplaced, false, false},
		// The LoggedOut handler already published the event with WhatsApp's reason.
		{clientregistry.StopLogout, true, false},
		{clientregistry.StopQRTimeout, true, true},
		{clientregistry.StopRemove, true, true},
	}
	for _, c := range cases {
		updateDB, publish := stopEffects(c.reason)
		if updateDB != c.updateDB || publish != c.publishLoggedOut {
			t.Errorf("stopEffects(%q) = (%v, %v), want (%v, %v)", c.reason, updateDB, publish, c.updateDB, c.publishLoggedOut)
		}
	}
}

func TestDisconnectStopReason(t *testing.T) {
	cases := []struct {
		paired  bool
		current bool
		want    clientregistry.StopReason
	}{
		{paired: true, current: true, want: clientregistry.StopReconnect},
		// QR socket closed by the server: stay stopped, no new QR cycle.
		{paired: false, current: true, want: clientregistry.StopQRTimeout},
		// Late event from an old client must never reconnect the instance.
		{paired: true, current: false, want: clientregistry.StopReplaced},
		{paired: false, current: false, want: clientregistry.StopReplaced},
	}
	for _, c := range cases {
		if got := disconnectStopReason(c.paired, c.current); got != c.want {
			t.Errorf("disconnectStopReason(paired=%v, current=%v) = %q, want %q", c.paired, c.current, got, c.want)
		}
	}
}

// Mirrors what the Disconnected handler and ReconnectClient do when the link
// drops: every paired instance gets Disconnected at once, sometimes twice.
// Each instance must reconnect exactly once and end with one live execution.
func TestLinkDropReconnectsEachInstanceOnce(t *testing.T) {
	const instances = 30
	registry := clientregistry.New()
	executions := make([]*clientregistry.Execution, instances)
	for i := range executions {
		executions[i] = clientregistry.NewExecution(fmt.Sprintf("inst-%d", i), nil, nil)
		registry.Register(executions[i])
	}

	var reconnects atomic.Int32
	var wg sync.WaitGroup
	onDisconnected := func(e *clientregistry.Execution) {
		defer wg.Done()
		reason := disconnectStopReason(true, registry.IsCurrent(e))
		if !e.Stop(reason) || reason != clientregistry.StopReconnect {
			return
		}
		reconnects.Add(1)
		// ReconnectClient -> StartInstance -> StartClient registers a new execution.
		registry.Register(clientregistry.NewExecution(e.InstanceID(), nil, nil))
		registry.RemoveIf(e)
	}

	for _, e := range executions {
		wg.Add(2)
		go onDisconnected(e)
		go onDisconnected(e) // duplicate event for the same socket
	}
	wg.Wait()

	if got := reconnects.Load(); got != instances {
		t.Fatalf("reconnects = %d, want %d", got, instances)
	}
	if got := registry.Count(); got != instances {
		t.Fatalf("live executions = %d, want %d", got, instances)
	}
}
