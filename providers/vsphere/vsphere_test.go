package vsphere_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	glog "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/vsphere"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/types"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
)

type ConfigurationTest struct {
	vsphere.Configuration
	CloudInit interface{}               `json:"cloud-init"`
	SSH       types.AutoScalerServerSSH `json:"ssh"`
	VM        string                    `json:"old-vm"`
	New       *NewVirtualMachineConf    `json:"new-vm"`
	inited    bool
	provider  providers.ProviderConfiguration
}

type NewVirtualMachineConf struct {
	Name       string
	Annotation string
	Memory     int
	CPUS       int
	Disk       int
	Network    *vsphere.Network
}

var testConfig ConfigurationTest
var confName = "../test/vsphere.json"

const (
	cantFindInstanceName = "Can't find instance named:%s"
	cantGetStatus        = "Can't get status on VM"
)

func (config *ConfigurationTest) WaitSSHReady(nodename, address string) error {
	return nil
}

func loadFromJson(fileName string) *ConfigurationTest {
	if !testConfig.inited {
		if configStr, err := os.ReadFile(fileName); err != nil {
			glog.Fatalf("failed to open config file:%s, error:%v", fileName, err)
		} else {
			err = json.Unmarshal(configStr, &testConfig)

			if err != nil {
				glog.Fatalf("failed to decode config file:%s, error:%v", fileName, err)
			}
		}

		testConfig.provider, _ = vsphere.NewVSphereProviderConfiguration("kubeadm", &testConfig.Configuration)
		testConfig.inited = true
	}

	return &testConfig
}

func Test_AuthMethodKey(t *testing.T) {
	if utils.ShouldTestFeature("Test_AuthMethodKey") {
		config := loadFromJson(confName)

		_, err := utils.AuthMethodFromPrivateKeyFile(config.SSH.GetAuthKeys())

		if assert.NoError(t, err) {
			t.Log("OK")
		}
	}
}

func Test_Sudo(t *testing.T) {
	if utils.ShouldTestFeature("Test_Sudo") {
		config := loadFromJson(confName)

		out, err := utils.Sudo(&config.SSH, "localhost", 30*time.Second, "ls")

		if assert.NoError(t, err) {
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
		config := loadFromJson(confName)

		if _, err := config.provider.AttachInstance(config.New.Name); err != nil {
			assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.New.Name))
		}
	}
}

func Test_createVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_createVM") {
		config := loadFromJson(confName)

		machine := providers.MachineCharacteristic{
			Memory:   config.New.Memory,
			Vcpu:     config.New.CPUS,
			DiskSize: config.New.Disk,
		}

		_, err := config.provider.InstanceCreate(config.New.Name, 0, "", config.SSH.GetUserName(), config.SSH.GetAuthKeys(), config.CloudInit, &machine)

		if assert.NoError(t, err, "Can't create VM") {
			t.Logf("VM created")
		}
	}
}

func Test_statusVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_statusVM") {
		config := loadFromJson(confName)

		if instance, err := config.provider.AttachInstance(config.New.Name); err == nil {
			status, err := instance.InstanceStatus(config.New.Name)

			if assert.NoError(t, err, "Can't get status VM") {
				t.Logf("The power of vm %s is:%v", config.New.Name, status.Powered())
			}
		} else {
			assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.New.Name))
		}
	}
}

func Test_powerOnVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_powerOnVM") {
		config := loadFromJson(confName)

		if instance, err := config.provider.AttachInstance(config.New.Name); assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.New.Name)) {
			if status, err := instance.InstanceStatus(config.New.Name); assert.NoError(t, err, cantGetStatus) && status.Powered() == false {
				err = instance.InstancePowerOn(config.New.Name)

				if assert.NoError(t, err, "Can't power on VM") {
					ipaddr, err := instance.InstanceWaitReady(config)

					if assert.NoError(t, err, "Can't get IP") {
						t.Logf("VM powered with IP:%s", ipaddr)
					}
				}
			}
		}
	}
}

func Test_powerOffVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_powerOffVM") {
		config := loadFromJson(confName)

		if instance, err := config.provider.AttachInstance(config.New.Name); assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.New.Name)) {
			if status, err := instance.InstanceStatus(config.New.Name); assert.NoError(t, err, cantGetStatus) && status.Powered() {
				err = instance.InstancePowerOff(config.New.Name)

				if assert.NoError(t, err, "Can't power off VM") {
					t.Logf("VM shutdown")
				}
			}
		}
	}
}

func Test_shutdownGuest(t *testing.T) {
	if utils.ShouldTestFeature("Test_shutdownGuest") {
		config := loadFromJson(confName)

		if instance, err := config.provider.AttachInstance(config.New.Name); assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.New.Name)) {
			if status, err := instance.InstanceStatus(config.New.Name); assert.NoError(t, err, cantGetStatus) && status.Powered() {
				err = instance.InstanceShutdownGuest(config.New.Name)

				if assert.NoError(t, err, "Can't power off VM") {
					t.Logf("VM shutdown")
				}
			}
		}
	}
}

func Test_deleteVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_deleteVM") {
		config := loadFromJson(confName)

		if instance, err := config.provider.AttachInstance(config.New.Name); assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.New.Name)) {
			err := instance.InstanceDelete(config.New.Name)

			if assert.NoError(t, err, "Can't delete VM") {
				t.Logf("VM deleted")
			}
		}
	}
}
