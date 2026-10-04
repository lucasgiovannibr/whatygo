package send_service

import (
	"os"
	"testing"

	"github.com/lucasgiovannibr/whatygo/pkg/utils"
)

// The tests serve files from httptest servers on 127.0.0.1, which the outbound policy
// refuses by default (SSRF protection); allow it for the whole package.
func TestMain(m *testing.M) {
	utils.SetAllowPrivateURLs(true)
	os.Exit(m.Run())
}
