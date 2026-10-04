package whatsmeow_service

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/patrickmn/go-cache"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_repository "github.com/lucasgiovannibr/whatygo/pkg/instance/repository"
	"github.com/lucasgiovannibr/whatygo/pkg/safemap"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
)

// stopRecorder is the service as far as stopping an instance goes.
type stopRecorder struct {
	*webhookCapture
	mu      sync.Mutex
	cleared []string
}

func (s *stopRecorder) ClearInstanceCache(id, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleared = append(s.cleared, id)
	return nil
}

func (s *stopRecorder) clearedIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.cleared...)
}

// qrRepo records what the lifecycle writes about the instance.
type qrRepo struct {
	instance_repository.InstanceRepository
	mu      sync.Mutex
	status  []string // "connected=false reason=..."
	jids    []string
	qrcodes []string
}

func (r *qrRepo) UpdateQrcode(_ string, qr string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.qrcodes = append(r.qrcodes, qr)
	return nil
}

func (r *qrRepo) UpdateConnected(_ string, connected bool, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := "connected=false"
	if connected {
		state = "connected=true"
	}
	r.status = append(r.status, state+" reason="+reason)
	return nil
}

func (r *qrRepo) UpdateJid(_ string, jid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jids = append(r.jids, jid)
	return nil
}

func lifecycleClient(t *testing.T, cfg *config.Config) (*MyClient, *stopRecorder, *qrRepo, chan bool, *webhookCapture) {
	t.Helper()
	mycli, capture := newHandlerClient(t, cfg)
	rec := &stopRecorder{webhookCapture: capture}
	repo := &qrRepo{}
	kill := make(chan bool, 1)
	mycli.service = rec
	mycli.instanceRepository = repo
	mycli.killChannel = safemap.New[chan bool]()
	mycli.killChannel.Set("inst-1", kill)
	mycli.userInfoCache = cache.New(time.Minute, time.Minute)
	return mycli, rec, repo, kill, capture
}

func publishedEvents(c *webhookCapture, want int) []string {
	var got []string
	deadline := time.After(2 * time.Second)
	for len(got) < want {
		select {
		case p := <-c.published:
			got = append(got, p.body["event"].(string))
		case <-deadline:
			return got
		}
	}
	return got
}

// The QR codes running out used to send `true` on the kill channel, which means "restart": the
// supervisor reported a LoggedOut that never happened and started a new client, the code counter
// began again at #1, and QRCODE_MAX_COUNT never gave up. Now the runtime is stopped for good.
func TestQRCodesRunningOutStopTheInstance(t *testing.T) {
	mycli, rec, repo, kill, capture := lifecycleClient(t, &config.Config{QrcodeMaxCount: 5})

	mycli.teardownQR("", false)

	if got := rec.clearedIDs(); len(got) != 1 || got[0] != "inst-1" {
		t.Fatalf("the instance must be stopped for good, cleared %v", got)
	}
	select {
	case v := <-kill:
		t.Fatalf("nothing may be sent on the kill channel (it would restart the client), got %v", v)
	default:
	}
	if len(repo.status) != 1 || repo.status[0] != "connected=false reason="+instance_repository.QRTimeoutReason {
		t.Fatalf("the reason must be stored, got %v", repo.status)
	}
	if events := publishedEvents(capture, 1); len(events) != 1 || events[0] != "QRTimeout" {
		t.Fatalf("QRTimeout is announced, and nothing else: %v", events)
	}
}

func TestMaxQRCountIsStoredAndAnnounced(t *testing.T) {
	mycli, rec, repo, _, capture := lifecycleClient(t, &config.Config{QrcodeMaxCount: 5})

	mycli.teardownQR("Maximum QR code count (5) reached", true)

	if len(rec.clearedIDs()) != 1 {
		t.Fatal("the instance must be stopped")
	}
	if len(repo.status) != 1 || repo.status[0] != "connected=false reason=Maximum QR code count (5) reached" {
		t.Fatalf("got %v", repo.status)
	}
	p := waitPublished(t, capture)
	data := p.body["data"].(map[string]interface{})
	if p.body["event"] != "QRTimeout" || data["reason"] != "Maximum QR code count (5) reached" || data["forceLogout"] != true {
		t.Fatalf("unexpected: %#v", p.body)
	}
}

// QRCODE_MAX_COUNT=0 turns the limit off: the instance keeps asking for new codes, as before.
func TestWithoutAQRLimitTheInstanceKeepsRestarting(t *testing.T) {
	mycli, rec, repo, kill, _ := lifecycleClient(t, &config.Config{QrcodeMaxCount: 0})

	mycli.teardownQR("", false)

	select {
	case v := <-kill:
		if !v {
			t.Fatal("the restart signal is `true`")
		}
	default:
		t.Fatal("without a limit the supervisor must be told to restart")
	}
	if len(rec.clearedIDs()) != 0 || len(repo.status) != 0 {
		t.Fatalf("nothing is stopped or stored: %v %v", rec.clearedIDs(), repo.status)
	}
}

// Logged out from the phone: the instance has no device any more. It used to be restarted at
// once as a new unpaired device (a new QR cycle for an account that had just left) and the
// handler waited up to 10 s for the supervisor to take the signal.
func TestLoggedOutFromThePhoneStopsTheInstanceAndForgetsTheDevice(t *testing.T) {
	mycli, rec, repo, kill, capture := lifecycleClient(t, &config.Config{QrcodeMaxCount: 5})

	start := time.Now()
	mycli.myEventHandler(&events.LoggedOut{OnConnect: true, Reason: events.ConnectFailureLoggedOut})
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the handler must not wait for the supervisor: %v", took)
	}

	if got := rec.clearedIDs(); len(got) != 1 || got[0] != "inst-1" {
		t.Fatalf("the instance must be stopped, cleared %v", got)
	}
	select {
	case v := <-kill:
		t.Fatalf("no restart signal, got %v", v)
	default:
	}
	if len(repo.jids) != 1 || repo.jids[0] != "" {
		t.Fatalf("the jid must be cleared, got %q", repo.jids)
	}
	events := publishedEvents(capture, 2)
	if len(events) == 0 || events[0] != "LoggedOut" {
		t.Fatalf("LoggedOut must be announced: %v", events)
	}
	if len(events) > 1 {
		t.Fatalf("LoggedOut must be announced once, got %v", events)
	}
}

// A paired instance that lost its connection may be started by a request; one without a paired
// device may not (there is nothing to send with, and starting it begins a new QR cycle).
func TestCanAutoStartRefusesAnInstanceWithoutADevice(t *testing.T) {
	w := autoStartService(t, false, instance_repository.QRTimeoutReason, "")
	if err := w.CanAutoStart(testInstanceID); !errors.Is(err, utils.ErrNotLoggedIn) {
		t.Fatalf("an instance without a paired device must not be started by a request, got %v", err)
	}
}
