package desktop

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRSAPubKey(t *testing.T) {
	privKey := os.Getenv("HOME") + "/.ssh/id_rsa"

	pub, err := generatePublicKey(privKey)

	if assert.NoError(t, err) {
		t.Logf("Found pub key: %s", pub)
	}
}
