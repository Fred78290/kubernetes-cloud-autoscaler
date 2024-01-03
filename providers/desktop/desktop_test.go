package desktop_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	glog "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/desktop"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/vsphere"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
	"github.com/joho/godotenv"
)

type ConfigurationTest struct {
	providers.BasicConfiguration
	provider providers.ProviderConfiguration
	inited   bool
}

var testConfig ConfigurationTest

const (
	cantFindInstanceName = "Can't find instance named:%s"
	cantGetStatus        = "Can't get status on VM"
)

func (config *ConfigurationTest) WaitSSHReady(nodename, address string) error {
	return nil
}

func getProviderConfFile() string {
	if config := os.Getenv("TEST_DESKTOP_CONFIG"); config != "" {
		return config
	}

	return "../test/providers/dekstop.json"
}

func getTestFile() string {
	if config := os.Getenv("TEST_CONFIG"); config != "" {
		return config
	}

	return "../test/desktop.json"
}

func loadFromJson() *ConfigurationTest {
	if !testConfig.inited {
		godotenv.Overload("../.env")

		if content, err := providers.LoadTextEnvSubst(getTestFile()); err != nil {
			glog.Fatalf("failed to open config file:%s, error:%v", getTestFile(), err)
		} else if json.NewDecoder(strings.NewReader(content)).Decode(&testConfig.BasicConfiguration); err != nil {
			glog.Fatalf("failed to decode config file:%s, error:%v", getTestFile(), err)
		} else if testConfig.provider, err = desktop.NewDesktopProviderConfiguration(getProviderConfFile()); err != nil {
			glog.Fatalf("failed to open config file:%s, error:%v", getProviderConfFile(), err)
		} else {
			testConfig.inited = true
		}
	}

	return &testConfig
}

func Test_AuthMethodKey(t *testing.T) {
	if utils.ShouldTestFeature("Test_AuthMethodKey") {
		config := loadFromJson()

		if _, err := utils.AuthMethodFromPrivateKeyFile(config.SSH.GetAuthKeys()); assert.NoError(t, err) {
			t.Log("OK")
		}
	}
}

func Test_Sudo(t *testing.T) {
	if utils.ShouldTestFeature("Test_Sudo") {
		config := loadFromJson()

		if out, err := utils.Sudo(&config.SSH, "localhost", 30*time.Second, "ls"); assert.NoError(t, err) {
			t.Log(out)
		}
	}
}

func Test_CIDR(t *testing.T) {
	if utils.ShouldTestFeature("Test_CIDR") {

		cidr := vsphere.ToCIDR("10.65.4.201", "255.255.255.0")

		if assert.Equal(t, cidr, "10.65.4.201/24") {
			cidr := vsphere.ToCIDR("10.65.4.201", "")
			assert.Equal(t, cidr, "10.65.4.201/8")
		}
	}
}

func Test_getVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_getVM") {
		config := loadFromJson()

		if _, err := config.provider.AttachInstance(config.InstanceName, 0); err != nil {
			assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.InstanceName))
		}
	}
}

func Test_createVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_createVM") {
		config := loadFromJson()

		if machine, found := config.Machines[config.InstanceType]; assert.True(t, found, fmt.Sprintf("machine: %s not found", config.InstanceType)) {
			if handler, err := config.provider.CreateInstance(config.InstanceName, config.InstanceType, 0); assert.NoError(t, err, "Can't create VM") && err == nil {

				createInput := &providers.InstanceCreateInput{
					NodeGroup: config.NodeGroup,
					UserName:  config.SSH.UserName,
					AuthKey:   config.SSH.AuthKeys,
					DiskSize:  config.DiskSize,
					CloudInit: nil,
					Machine:   &machine,
				}

				if vmuuid, err := handler.InstanceCreate(createInput); assert.NoError(t, err, "Can't create VM") {
					t.Logf("VM created: %s", vmuuid)
				}
			}
		}
	}
}

func Test_statusVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_statusVM") {
		config := loadFromJson()

		if instance, err := config.provider.AttachInstance(config.InstanceName, 0); err == nil {
			if status, err := instance.InstanceStatus(); assert.NoError(t, err, "Can't get status VM") {
				t.Logf("The power of vm %s is:%v", config.InstanceName, status.Powered())
			}
		} else {
			assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.InstanceName))
		}
	}
}

func Test_powerOnVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_powerOnVM") {
		config := loadFromJson()

		if instance, err := config.provider.AttachInstance(config.InstanceName, 0); assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.InstanceName)) {
			if status, err := instance.InstanceStatus(); assert.NoError(t, err, cantGetStatus) && status.Powered() == false {
				if err = instance.InstancePowerOn(); assert.NoError(t, err, "Can't power on VM") {
					if ipaddr, err := instance.InstanceWaitReady(config); assert.NoError(t, err, "Can't get IP") {
						t.Logf("VM powered with IP:%s", ipaddr)
					}
				}
			}
		}
	}
}

func Test_powerOffVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_powerOffVM") {
		config := loadFromJson()

		if instance, err := config.provider.AttachInstance(config.InstanceName, 0); assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.InstanceName)) {
			if status, err := instance.InstanceStatus(); assert.NoError(t, err, cantGetStatus) && status.Powered() {
				if err = instance.InstancePowerOff(); assert.NoError(t, err, "Can't power off VM") {
					t.Logf("VM shutdown")
				}
			}
		}
	}
}

func Test_shutdownGuest(t *testing.T) {
	if utils.ShouldTestFeature("Test_shutdownGuest") {
		config := loadFromJson()

		if instance, err := config.provider.AttachInstance(config.InstanceName, 0); assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.InstanceName)) {
			if status, err := instance.InstanceStatus(); assert.NoError(t, err, cantGetStatus) && status.Powered() {
				if err = instance.InstanceShutdownGuest(); assert.NoError(t, err, "Can't power off VM") {
					t.Logf("VM shutdown")
				}
			}
		}
	}
}

func Test_deleteVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_deleteVM") {
		config := loadFromJson()

		if instance, err := config.provider.AttachInstance(config.InstanceName, 0); assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.InstanceName)) {
			if err := instance.InstanceDelete(); assert.NoError(t, err, "Can't delete VM") {
				t.Logf("VM deleted")
			}
		}
	}
}
