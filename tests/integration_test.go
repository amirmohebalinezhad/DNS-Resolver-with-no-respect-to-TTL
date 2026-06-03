package tests

import (
	"os"
	"testing"
)

func TestIntegrationOptIn(t *testing.T) {
	if os.Getenv("DNS_SWR_INTEGRATION") != "1" {
		t.Skip("set DNS_SWR_INTEGRATION=1 and run a local resolver scenario for integration tests")
	}
}
