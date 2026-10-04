package whatsmeow_service

import (
	"sync"
	"testing"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
)

// The event handler reads the instance record while UpdateInstanceSettings replaces it from
// an API goroutine. Run with -race: a plain field made this a data race.
func TestInstanceRecordCanBeReplacedWhileTheHandlerReadsIt(t *testing.T) {
	c := clientFor(&instance_model.Instance{Id: "a", Webhook: "w0"})

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if c.inst() == nil || c.inst().Id != "a" {
						t.Error("instance record lost")
						return
					}
					_ = c.inst().Webhook
				}
			}
		}()
	}
	for i := 0; i < 2000; i++ {
		c.setInst(&instance_model.Instance{Id: "a", Webhook: "w"})
	}
	close(stop)
	wg.Wait()
}

// StartClient used to rewrite whatsmeow's global identity for every instance; starting
// several at once was a data race, and the version was re-applied each time.
func TestConfigureWAIdentityIsRaceFreeAndIdempotent(t *testing.T) {
	cfg := &config.Config{LogDirectory: t.TempDir(), WhatsappVersionMajor: 2, WhatsappVersionMinor: 3000, WhatsappVersionPatch: 1234567}
	w := whatsmeowService{config: cfg, loggerWrapper: logger_wrapper.NewLoggerManagerForTest(t, cfg)}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cd := &ClientData{Instance: &instance_model.Instance{Id: "i", OsName: "Linux"}}
			w.configureWAIdentity(cd, i%2 == 0)
		}(i)
	}
	wg.Wait()

	waIdentityMu.Lock()
	defer waIdentityMu.Unlock()
	if !waIdentityApplied || appliedWAVersion != (clientVersion{2, 3000, 1234567}) {
		t.Fatalf("version not applied: %+v", appliedWAVersion)
	}
}
