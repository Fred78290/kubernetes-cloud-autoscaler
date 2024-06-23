package utils

import (
	"testing"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/sshutils"
	glog "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

type configurationTest struct {
	SSH        sshutils.AutoScalerServerSSH `json:"ssh"`
	inited     bool
	parentTest *testing.T
	childTest  *testing.T
}

var testConfig configurationTest

func loadFromJson(t *testing.T) *configurationTest {
	if !testConfig.inited {
		var err error

		configFile := GetConfigFile()

		if err = LoadConfig(configFile, &testConfig); err != nil {
			glog.Fatalf("failed to decode test config file: %s, error: %v", configFile, err)
		}

		testConfig.inited = true
		testConfig.parentTest = t
		testConfig.childTest = t
	}

	return &testConfig
}

func Test_AuthMethodKey(t *testing.T) {
	if ShouldTestFeature("Test_AuthMethodKey") {
		config := loadFromJson(t)

		_, err := AuthMethodFromPrivateKeyFile(config.SSH.GetAuthKeys())

		if assert.NoError(t, err) {
			t.Log("OK")
		}
	}
}

func Test_CIDR(t *testing.T) {
	if ShouldTestFeature("Test_CIDR") {

		cidr := cloudinit.ToCIDR("10.65.4.201", "255.255.255.0")

		if assert.Equal(t, cidr, "10.65.4.201/24") {
			cidr := cloudinit.ToCIDR("10.65.4.201", "")
			assert.Equal(t, cidr, "10.65.4.201/8")
		}
	}
}

func Test_Sudo(t *testing.T) {
	if ShouldTestFeature("Test_Sudo") {
		config := loadFromJson(t)

		out, err := Sudo(&config.SSH, "localhost", 10, "ls")

		if assert.NoError(t, err) {
			t.Log(out)
		}
	}
}
