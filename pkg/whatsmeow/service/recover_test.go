package whatsmeow_service

import (
	"testing"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
)

func TestRecoverAndLogContainsPanic(t *testing.T) {
	lw := logger_wrapper.NewLoggerManagerForTest(t, &config.Config{LogDirectory: t.TempDir()})

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer recoverAndLog(lw, "inst", "test")
		panic("boom")
	}()
	<-done // reaching here means the panic did not crash the test process
}
