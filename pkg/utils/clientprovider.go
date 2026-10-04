package utils

import (
	"context"
	"errors"
	"time"

	"go.mau.fi/whatsmeow"

	"github.com/lucasgiovannibr/whatygo/pkg/safemap"
)

// The errors every service reports when the instance has no usable client. They used to be
// built with errors.New in nine copies of the same function, so callers could only compare
// text.
var (
	// ErrNoActiveSession: the instance has no client, or it did not come up.
	ErrNoActiveSession = errors.New("no active session found")
	// ErrClientDisconnected: the instance has a client whose socket is down.
	ErrClientDisconnected = errors.New("client disconnected")
	// ErrNotLoggedIn: the client has no paired device, so it can never send.
	ErrNotLoggedIn = errors.New("instance is not logged in: pair the device (QR code or pairing code) first")
	// ErrDisconnectedByUser: the instance was disconnected through the API; it is only
	// started again by an explicit connect, not by the next request that needs it.
	ErrDisconnectedByUser = errors.New("instance was disconnected: connect it first (POST /instance/connect)")
	// ErrOwnedElsewhere: another replica runs this instance.
	ErrOwnedElsewhere = errors.New("instance is running on another replica")
)

// ClientStarter starts instances. whatsmeow_service.WhatsmeowService implements it.
type ClientStarter interface {
	// StartInstance starts the instance's client in the background.
	StartInstance(instanceId string) error
	// CanAutoStart reports whether a request may start the instance on its own (nil), or why
	// it may not.
	CanAutoStart(instanceId string) error
}

// ClientLogger is the part of the instance logger EnsureClient uses.
type ClientLogger interface {
	LogDebug(format string, args ...interface{})
	LogInfo(format string, args ...interface{})
	LogError(format string, args ...interface{})
}

// ClientProvider returns the connected client of an instance, starting the instance when it
// has none: the one implementation behind every service's ensureClientConnected.
type ClientProvider struct {
	Clients *safemap.Map[*whatsmeow.Client]
	Starter ClientStarter
	// Gate, when set, is consulted before a request starts an instance by itself. Explicit
	// management calls (connect, reconnect...) leave it unset.
	Gate bool
	// RequirePaired makes a connected client without a paired device an error
	// (ErrNotLoggedIn). The send paths want it; instance management (QR, status, logout)
	// must still reach an unpaired client.
	RequirePaired bool
	// Wait is how long to wait for a started instance to connect (default
	// InstanceStartTimeout).
	Wait time.Duration
}

// Ensure returns the connected client of the instance. With RequirePaired, a client without
// a paired device is an error (#77: it would otherwise spend ~80 s of reconnect cycles
// before failing).
func (p ClientProvider) Ensure(ctx context.Context, instanceID string, log ClientLogger) (*whatsmeow.Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	wait := p.Wait
	if wait <= 0 {
		wait = InstanceStartTimeout
	}

	client := p.Clients.Get(instanceID)
	log.LogDebug("[%s] Checking client connection status - Client exists: %v", instanceID, client != nil)

	switch {
	case client == nil:
		if p.Gate {
			if err := p.Starter.CanAutoStart(instanceID); err != nil {
				log.LogInfo("[%s] Not starting the instance on a request: %v", instanceID, err)
				return nil, err
			}
		}
		log.LogInfo("[%s] No client found, attempting to start new instance", instanceID)
		if err := p.Starter.StartInstance(instanceID); err != nil {
			log.LogError("[%s] Failed to start instance: %v", instanceID, err)
			if errors.Is(err, ErrOwnedElsewhere) {
				return nil, err
			}
			return nil, ErrNoActiveSession
		}

		log.LogInfo("[%s] Instance started, waiting for the connection...", instanceID)
		client = waitForClientCtx(ctx, func() *whatsmeow.Client { return p.Clients.Get(instanceID) }, wait)
		if client == nil || !client.IsConnected() {
			log.LogError("[%s] New client validation failed - Exists: %v, Connected: %v", instanceID, client != nil, client != nil && client.IsConnected())
			return nil, ErrNoActiveSession
		}
	case !client.IsConnected():
		log.LogError("[%s] Existing client is disconnected", instanceID)
		return nil, ErrClientDisconnected
	}

	if p.RequirePaired && (client.Store == nil || client.Store.ID == nil) {
		log.LogInfo("[%s] Client is connected but has no paired device", instanceID)
		return nil, ErrNotLoggedIn
	}
	log.LogDebug("[%s] Client successfully validated - Connected: %v", instanceID, client.IsConnected())
	return client, nil
}

// waitForClientCtx is WaitForClient that also stops when ctx is done.
func waitForClientCtx(ctx context.Context, get func() *whatsmeow.Client, timeout time.Duration) *whatsmeow.Client {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	done := make(chan *whatsmeow.Client, 1)
	go func() { done <- WaitForClient(get, timeout) }()
	select {
	case c := <-done:
		return c
	case <-ctx.Done():
		return get()
	}
}
