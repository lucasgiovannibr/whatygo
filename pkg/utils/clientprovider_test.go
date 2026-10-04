package utils

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"

	"github.com/lucasgiovannibr/whatygo/pkg/safemap"
)

type fakeStarter struct {
	startErr, gateErr error
	started           int
}

func (f *fakeStarter) StartInstance(string) error { f.started++; return f.startErr }
func (f *fakeStarter) CanAutoStart(string) error  { return f.gateErr }

type nopLog struct{}

func (nopLog) LogDebug(string, ...interface{}) {}
func (nopLog) LogInfo(string, ...interface{})  {}
func (nopLog) LogError(string, ...interface{}) {}

func provider(st *fakeStarter, gate bool) (ClientProvider, *safemap.Map[*whatsmeow.Client]) {
	clients := safemap.New[*whatsmeow.Client]()
	return ClientProvider{Clients: clients, Starter: st, Gate: gate, Wait: 100 * time.Millisecond}, clients
}

func TestEnsureRefusesToAutoStartAnInstanceTheUserDisconnected(t *testing.T) {
	st := &fakeStarter{gateErr: ErrDisconnectedByUser}
	p, _ := provider(st, true)
	if _, err := p.Ensure(context.Background(), "i", nopLog{}); !errors.Is(err, ErrDisconnectedByUser) {
		t.Fatalf("got %v", err)
	}
	if st.started != 0 {
		t.Fatal("must not start the instance")
	}
}

// Explicit management calls (connect, reconnect...) have no gate.
func TestEnsureWithoutGateStartsAnyway(t *testing.T) {
	st := &fakeStarter{gateErr: ErrDisconnectedByUser}
	p, _ := provider(st, false)
	_, err := p.Ensure(context.Background(), "i", nopLog{})
	if st.started != 1 {
		t.Fatalf("started %d times", st.started)
	}
	if !errors.Is(err, ErrNoActiveSession) { // the fake never creates a client
		t.Fatalf("got %v", err)
	}
}

func TestEnsureStartFailureIsNoActiveSessionButOwnershipIsKept(t *testing.T) {
	p, _ := provider(&fakeStarter{startErr: errors.New("db down")}, true)
	if _, err := p.Ensure(context.Background(), "i", nopLog{}); !errors.Is(err, ErrNoActiveSession) {
		t.Fatalf("got %v", err)
	}
	p, _ = provider(&fakeStarter{startErr: ErrOwnedElsewhere}, true)
	if _, err := p.Ensure(context.Background(), "i", nopLog{}); !errors.Is(err, ErrOwnedElsewhere) {
		t.Fatalf("got %v", err)
	}
}

func TestEnsureExistingButDisconnectedClient(t *testing.T) {
	p, clients := provider(&fakeStarter{}, true)
	clients.Set("i", whatsmeow.NewClient(&store.Device{}, nil))
	if _, err := p.Ensure(context.Background(), "i", nopLog{}); !errors.Is(err, ErrClientDisconnected) {
		t.Fatalf("got %v", err)
	}
}
