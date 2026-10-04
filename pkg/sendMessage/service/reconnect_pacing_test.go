package send_service

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"github.com/lucasgiovannibr/whatygo/pkg/safemap"
	whatsmeow_service "github.com/lucasgiovannibr/whatygo/pkg/whatsmeow/service"
)

// pacedFake counts how a send asks for a reconnection.
type pacedFake struct {
	whatsmeow_service.WhatsmeowService
	requested atomic.Int32 // RequestReconnect: the paced way
	forced    atomic.Int32 // ReconnectClient: unpaced, what a send used to call
}

func (f *pacedFake) StartInstance(string) error   { return nil }
func (f *pacedFake) CanAutoStart(string) error    { return nil }
func (f *pacedFake) RequestReconnect(string) bool { f.requested.Add(1); return true }
func (f *pacedFake) ReconnectClient(string) error { f.forced.Add(1); return nil }

// A send that finds the instance disconnected asks for the reconnection through the backoff
// (RequestReconnect) and never forces one itself: with traffic arriving, the forced one made an
// instance that WhatsApp keeps dropping reconnect every ~15 s.
func TestSendOnADisconnectedInstanceDoesNotForceReconnects(t *testing.T) {
	old := reconnectRetryStep
	reconnectRetryStep = time.Millisecond
	defer func() { reconnectRetryStep = old }()

	cfg := &config.Config{LogDirectory: t.TempDir()}
	clients := safemap.New[*whatsmeow.Client]()
	clients.Set("inst", whatsmeow.NewClient(&store.Device{}, nil)) // a client whose socket is down
	fake := &pacedFake{}
	s := &sendService{
		clientPointer:    clients,
		whatsmeowService: fake,
		config:           cfg,
		loggerWrapper:    logger_wrapper.NewLoggerManagerForTest(t, cfg),
	}

	_, err := s.ensureClientConnectedWithRetry("inst", 3)
	if !errors.Is(err, ErrClientDisconnected) {
		t.Fatalf("want the disconnection reported, got %v", err)
	}
	if got := fake.requested.Load(); got != 3 {
		t.Fatalf("the reconnection must be requested on each attempt, got %d", got)
	}
	if got := fake.forced.Load(); got != 0 {
		t.Fatalf("a send must not force ReconnectClient, it did %d times", got)
	}
}
