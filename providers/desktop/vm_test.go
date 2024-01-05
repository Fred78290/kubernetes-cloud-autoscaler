package desktop

import (
	"os"
	"testing"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/stretchr/testify/assert"
)

func TestRSAPubKey(t *testing.T) {
	privKey := os.Getenv("HOME") + "/.ssh/id_rsa"

	pub, err := cloudinit.GeneratePublicKey(privKey)

	if assert.NoError(t, err) {
		t.Logf("Found pub key: %s", pub)
	}
}
