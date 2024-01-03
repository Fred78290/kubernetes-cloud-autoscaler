package aws_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	glog "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/aws"
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
	cantFindEC2   = "Can't find ec2 instance named:%s"
	cantGetStatus = "Can't get status on VM"
)

func getProviderConfFile() string {
	if config := os.Getenv("TEST_AWS_CONFIG"); config != "" {
		return config
	}

	return "../test/providers/aws.json"
}

func getTestFile() string {
	if config := os.Getenv("TEST_CONFIG"); config != "" {
		return config
	}

	return "../test/aws.json"
}

func loadFromJson() *ConfigurationTest {
	if !testConfig.inited {
		godotenv.Overload("../.env")

		if content, err := providers.LoadTextEnvSubst(getTestFile()); err != nil {
			glog.Fatalf("failed to open config file:%s, error:%v", getTestFile(), err)
		} else if json.NewDecoder(strings.NewReader(content)).Decode(&testConfig.BasicConfiguration); err != nil {
			glog.Fatalf("failed to decode config file:%s, error:%v", getTestFile(), err)
		} else if testConfig.provider, err = aws.NewAwsProviderConfiguration(getProviderConfFile()); err != nil {
			glog.Fatalf("failed to open config file:%s, error:%v", getProviderConfFile(), err)
		} else {
			testConfig.inited = true
		}
	}

	return &testConfig
}

func (config *ConfigurationTest) WaitSSHReady(nodename, address string) error {
	return context.PollImmediate(time.Second, time.Duration(config.SSH.WaitSshReadyInSeconds)*time.Second, func() (done bool, err error) {
		// Set hostname
		if _, err := utils.Sudo(&config.SSH, address, time.Second, fmt.Sprintf("hostnamectl set-hostname %s", nodename)); err != nil {
			if strings.HasSuffix(err.Error(), "connection refused") || strings.HasSuffix(err.Error(), "i/o timeout") {
				return false, nil
			}

			return false, err
		}
		return true, nil
	})
}

func Test_AuthMethodKey(t *testing.T) {
	if utils.ShouldTestFeature("Test_AuthMethodKey") {
		config := loadFromJson()

		_, err := utils.AuthMethodFromPrivateKeyFile(config.SSH.GetAuthKeys())

		if assert.NoError(t, err) {
			t.Log("OK")
		}
	}
}

func Test_Sudo(t *testing.T) {
	if utils.ShouldTestFeature("Test_Sudo") {
		config := loadFromJson()

		out, err := utils.Sudo(&config.SSH, "localhost", 10, "ls")

		if assert.NoError(t, err) {
			t.Log(out)
		}
	}
}

func Test_getInstanceID(t *testing.T) {
	if utils.ShouldTestFeature("Test_getInstanceID") {
		config := loadFromJson()

		if instance, err := config.provider.AttachInstance(config.InstanceName, 0); err != nil {
			if !strings.HasPrefix(err.Error(), "unable to find VM:") {
				assert.NoError(t, err, fmt.Sprintf(cantFindEC2, config.InstanceName))
			}
		} else if assert.NotEmpty(t, instance) {
			status, err := instance.InstanceStatus()

			if assert.NoErrorf(t, err, "Can't get status of VM") {
				t.Logf("The power of vm is:%v", status.Powered())
			}
		}
	}
}

func Test_createInstance(t *testing.T) {
	if utils.ShouldTestFeature("Test_createInstance") {
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

func Test_statusInstance(t *testing.T) {
	if utils.ShouldTestFeature("Test_statusInstance") {
		config := loadFromJson()

		if instance, err := config.provider.AttachInstance(config.InstanceName, 0); assert.NoError(t, err, fmt.Sprintf(cantFindEC2, config.InstanceName)) {
			status, err := instance.InstanceStatus()

			if assert.NoError(t, err, "Can't get status VM") {
				t.Logf("The power of vm %s is:%v", config.InstanceName, status.Powered())
			}
		}
	}
}

func Test_waitForPowered(t *testing.T) {
	if utils.ShouldTestFeature("Test_waitForPowered") {
		config := loadFromJson()

		if instance, err := config.provider.AttachInstance(config.InstanceName, 0); assert.NoError(t, err, fmt.Sprintf(cantFindEC2, config.InstanceName)) {
			if status, err := instance.InstanceStatus(); assert.NoError(t, err, cantGetStatus) && status.Powered() {
				if err := instance.InstanceWaitForPowered(); assert.NoError(t, err, "Can't WaitForPowered") {
					t.Log("VM powered")
				}
			} else {
				t.Log("VM is not powered")
			}
		}
	}
}

func Test_waitForIP(t *testing.T) {
	if utils.ShouldTestFeature("Test_waitForIP") {
		config := loadFromJson()

		if instance, err := config.provider.AttachInstance(config.InstanceName, 0); assert.NoError(t, err, fmt.Sprintf(cantFindEC2, config.InstanceName)) {
			if status, err := instance.InstanceStatus(); assert.NoError(t, err, cantGetStatus) && status.Powered() {
				if ipaddr, err := instance.InstanceWaitReady(config); assert.NoError(t, err, "Can't get IP") {
					t.Logf("VM powered with IP:%s", ipaddr)
				}
			} else {
				t.Log("VM is not powered")
			}
		}
	}
}

func Test_powerOnInstance(t *testing.T) {
	if utils.ShouldTestFeature("Test_powerOnInstance") {
		config := loadFromJson()

		if instance, err := config.provider.AttachInstance(config.InstanceName, 0); assert.NoError(t, err, fmt.Sprintf(cantFindEC2, config.InstanceName)) {
			if status, err := instance.InstanceStatus(); assert.NoError(t, err, cantGetStatus) {
				if status.Powered() == false {
					if err = instance.InstancePowerOn(); assert.NoError(t, err, "Can't power on VM") {
						ipaddr, err := instance.InstanceWaitReady(config)

						if assert.NoError(t, err, "Can't get IP") {
							t.Logf("VM powered with IP:%s", ipaddr)
						}
					}
				} else {
					t.Logf("VM already powered with IP:%s", status.Address())
				}
			}
		}
	}
}

func Test_powerOffInstance(t *testing.T) {
	if utils.ShouldTestFeature("Test_powerOffInstance") {
		config := loadFromJson()

		if instance, err := config.provider.AttachInstance(config.InstanceName, 0); assert.NoError(t, err, fmt.Sprintf(cantFindEC2, config.InstanceName)) {
			if status, err := instance.InstanceStatus(); assert.NoError(t, err, cantGetStatus) && status.Powered() {
				if err = instance.InstancePowerOff(); assert.NoError(t, err, "Can't power off VM") {
					t.Logf("VM shutdown")
				}
			}
		}
	}
}

func Test_shutdownInstance(t *testing.T) {
	if utils.ShouldTestFeature("Test_shutdownInstance") {
		config := loadFromJson()

		if instance, err := config.provider.AttachInstance(config.InstanceName, 0); assert.NoError(t, err, fmt.Sprintf(cantFindEC2, config.InstanceName)) {
			if status, err := instance.InstanceStatus(); assert.NoError(t, err, cantGetStatus) && status.Powered() {
				if err = instance.InstanceShutdownGuest(); assert.NoError(t, err, "Can't power off VM") {
					t.Logf("VM shutdown")
				}
			}
		}
	}
}

func Test_deleteInstance(t *testing.T) {
	if utils.ShouldTestFeature("Test_deleteInstance") {
		config := loadFromJson()

		if instance, err := config.provider.AttachInstance(config.InstanceName, 0); assert.NoError(t, err, fmt.Sprintf(cantFindEC2, config.InstanceName)) {
			if assert.NotEmpty(t, instance) {
				if err := instance.InstanceDelete(); assert.NoError(t, err, "Can't delete VM") {
					t.Logf("VM deleted")
				}
			}
		}
	}
}
