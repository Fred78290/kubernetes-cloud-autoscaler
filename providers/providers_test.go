package providers_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	glog "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/aws"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/cloudstack"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/desktop"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/multipass"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/openstack"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers/vsphere"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/sshutils"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/utils"
)

type VMConfiguration struct {
	InstanceName string `json:"instance-name"`
	InstanceType string `json:"instance-type"`
	DiskSize     int    `default:"10240" json:"disk-size"`
	DiskType     string `default:"gp3" json:"disk-type"`
}

type BasicConfiguration struct {
	Plateform string                       `json:"plateform"`
	CloudInit cloudinit.CloudInit          `json:"cloud-init"`
	SSH       sshutils.AutoScalerServerSSH `json:"ssh-infos"`
	NodeGroup string                       `json:"nodegroup"`
}

type configurationTest struct {
	appConfig  BasicConfiguration
	vmConfig   VMConfiguration
	provider   providers.ProviderConfiguration
	machines   map[string]providers.MachineCharacteristic
	inited     bool
	parentTest *testing.T
	childTest  *testing.T
}

var testConfig configurationTest

const (
	cantFindInstanceName = "Can't find instance named: %s"
	cantGetStatus        = "Can't get status on VM"
)

func createProviderHandler(configFile, plateform string) (providerConfiguration providers.ProviderConfiguration, err error) {
	glog.Infof("Load %s provider config", plateform)

	switch plateform {
	case providers.AwsCloudProviderName:
		providerConfiguration, err = aws.NewAwsProviderConfiguration(configFile)
	case providers.VSphereCloudProviderName:
		providerConfiguration, err = vsphere.NewVSphereProviderConfiguration(configFile)
	case providers.VMWareWorkstationProviderName:
		providerConfiguration, err = desktop.NewDesktopProviderConfiguration(configFile)
	case providers.MultipassProviderName:
		providerConfiguration, err = multipass.NewMultipassProviderConfiguration(configFile)
	case providers.OpenStackProviderName:
		providerConfiguration, err = openstack.NewOpenStackProviderConfiguration(configFile)
	case providers.CloudStackProviderName:
		providerConfiguration, err = cloudstack.NewCloudStackProviderConfiguration(configFile)
	default:
		err = fmt.Errorf("Unsupported cloud provider: %s", plateform)
	}

	return
}

func loadFromJson(t *testing.T) *configurationTest {
	if !testConfig.inited {
		var err error

		serverFile := utils.GetServerConfigFile()
		configFile := utils.GetConfigFile()
		machineFile := utils.GetMachinesConfigFile()
		providerFile := utils.GetProviderConfigFile()
		testMode := utils.GetTestMode()

		if err = utils.LoadConfig(serverFile, &testConfig.appConfig); err != nil {
			glog.Fatalf("failed to decode test config file: %s, error: %v", configFile, err)
		}

		if err = utils.LoadConfig(configFile, &testConfig.vmConfig); err != nil {
			glog.Fatalf("failed to decode test config file: %s, error: %v", configFile, err)
		}

		if err = utils.LoadConfig(machineFile, &testConfig.machines); err != nil {
			glog.Fatalf("failed to open machines config file: %s, error: %v", machineFile, err)
		}

		if testConfig.provider, err = createProviderHandler(providerFile, testConfig.appConfig.Plateform); err != nil {
			glog.Fatalf("failed to open provider config file: %s, error: %v", providerFile, err)
		} else {
			testConfig.inited = true
		}

		testConfig.appConfig.SSH.SetMode(testMode)
		testConfig.provider.SetMode(testMode)

		testConfig.parentTest = t
		testConfig.childTest = t
	}

	return &testConfig
}

func (b *configurationTest) Child(t *testing.T) *configurationTest {
	b.childTest = t
	return b
}

func (b *configurationTest) RunningTest() *testing.T {
	return b.childTest
}

func (config *configurationTest) WaitSSHReady(nodename, address string) error {
	return context.PollImmediate(time.Second, time.Duration(config.appConfig.SSH.WaitSshReadyInSeconds)*time.Second, func() (done bool, err error) {
		// Set hostname
		if _, err := utils.Sudo(&config.appConfig.SSH, address, time.Second, fmt.Sprintf("hostnamectl set-hostname %s", nodename)); err != nil {
			errMessage := err.Error()

			if strings.HasSuffix(errMessage, "connection refused") || strings.HasSuffix(errMessage, "i/o timeout") || strings.Contains(errMessage, "handshake failed: EOF") || strings.Contains(errMessage, "connect: no route to host") {
				return false, nil
			}

			return false, err
		}
		return true, nil
	})
}

func (config *configurationTest) getVM() {
	t := config.RunningTest()

	if _, err := config.provider.AttachInstance(config.vmConfig.InstanceName, false, 0); err != nil {
		assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.vmConfig.InstanceName))
	}
}

func (config *configurationTest) createVM() {
	t := config.RunningTest()

	if machine, found := config.machines[config.vmConfig.InstanceType]; assert.True(t, found, fmt.Sprintf("machine: %s not found", config.vmConfig.InstanceType)) {
		if handler, err := config.provider.CreateInstance(config.vmConfig.InstanceName, config.vmConfig.InstanceType, false, 0); assert.NoError(t, err, "Can't create VM") && err == nil {

			createInput := &providers.InstanceCreateInput{
				NodeGroup: config.appConfig.NodeGroup,
				UserName:  config.appConfig.SSH.UserName,
				AuthKey:   config.appConfig.SSH.AuthKeys,
				CloudInit: nil,
				Machine:   &machine,
			}

			if vmuuid, err := handler.InstanceCreate(createInput); assert.NoError(t, err, "Can't create VM") {
				t.Logf("VM created: %s", vmuuid)
			}
		}
	}
}

func (config *configurationTest) statusVM() {
	t := config.RunningTest()

	if instance, err := config.provider.AttachInstance(config.vmConfig.InstanceName, false, 0); err == nil {
		if status, err := instance.InstanceStatus(); assert.NoError(t, err, "Can't get status VM") {
			t.Logf("The power of vm %s is: %v", config.vmConfig.InstanceName, status.Powered())
		}
	} else {
		assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.vmConfig.InstanceName))
	}
}

func (config *configurationTest) waitForPowered() {
	t := config.RunningTest()

	if instance, err := config.provider.AttachInstance(config.vmConfig.InstanceName, false, 0); assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.vmConfig.InstanceName)) {
		if status, err := instance.InstanceStatus(); assert.NoError(t, err, cantGetStatus) && status.Powered() {
			if err := instance.InstanceWaitForPowered(); assert.NoError(t, err, "Can't WaitForPowered") {
				t.Log("VM powered")
			}
		} else {
			t.Log("VM is not powered")
		}
	}
}

func (config *configurationTest) waitForIP() {
	t := config.RunningTest()

	if instance, err := config.provider.AttachInstance(config.vmConfig.InstanceName, false, 0); assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.vmConfig.InstanceName)) {
		if status, err := instance.InstanceStatus(); assert.NoError(t, err, cantGetStatus) && status.Powered() {
			if ipaddr, err := instance.InstanceWaitReady(config); assert.NoError(t, err, "Can't get IP") {
				t.Logf("VM powered with IP: %s", ipaddr)
			}
		} else {
			t.Log("VM is not powered")
		}
	}
}

func (config *configurationTest) powerOnVM() {
	t := config.RunningTest()

	if instance, err := config.provider.AttachInstance(config.vmConfig.InstanceName, false, 0); assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.vmConfig.InstanceName)) {
		if status, err := instance.InstanceStatus(); assert.NoError(t, err, cantGetStatus) {
			if status.Powered() == false {
				if err = instance.InstancePowerOn(); assert.NoError(t, err, "Can't power on VM") {
					if ready, err := instance.InstanceWaitForToolsRunning(); assert.Equal(t, true, ready, "vmware tools not ready") && assert.NoError(t, err, "vmware tools not running") {
						t.Log("Tools ready")
					}

					if ipaddr, err := instance.InstanceWaitReady(config); assert.NoError(t, err, "Can't get IP") {
						t.Logf("VM powered with IP: %s", ipaddr)
					}
				}
			} else {
				t.Logf("VM already powered with IP: %s", status.Address())
			}
		}
	}
}

func (config *configurationTest) powerOffVM() {
	t := config.RunningTest()

	if instance, err := config.provider.AttachInstance(config.vmConfig.InstanceName, false, 0); assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.vmConfig.InstanceName)) {
		if status, err := instance.InstanceStatus(); assert.NoError(t, err, cantGetStatus) && status.Powered() {
			if err = instance.InstancePowerOff(); assert.NoError(t, err, "Can't power off VM") {
				t.Logf("VM shutdown")
			}
		}
	}
}

func (config *configurationTest) shutdownVM() {
	t := config.RunningTest()

	if instance, err := config.provider.AttachInstance(config.vmConfig.InstanceName, false, 0); assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.vmConfig.InstanceName)) {
		if status, err := instance.InstanceStatus(); assert.NoError(t, err, cantGetStatus) && status.Powered() {
			if err = instance.InstanceShutdownGuest(); assert.NoError(t, err, "Can't power off VM") {
				t.Logf("VM shutdown")
			}
		}
	}
}

func (config *configurationTest) deleteVM() {
	t := config.RunningTest()

	if instance, err := config.provider.AttachInstance(config.vmConfig.InstanceName, false, 0); assert.NoError(t, err, fmt.Sprintf(cantFindInstanceName, config.vmConfig.InstanceName)) {
		if assert.NotEmpty(t, instance) {
			if err := instance.InstanceDelete(); assert.NoError(t, err, "Can't delete VM") {
				t.Logf("VM deleted")
			}
		}
	}
}

func Test_Providers(t *testing.T) {
	if os.Getenv("TESTLOGLEVEL") == "DEBUG" {
		glog.SetLevel(glog.DebugLevel)
	} else if os.Getenv("TESTLOGLEVEL") == "TRACE" {
		glog.SetLevel(glog.TraceLevel)
	}

	config := loadFromJson(t)

	if utils.ShouldTestFeature("Test_createVM") {
		t.Run("Test_createVM", func(t *testing.T) {
			config.Child(t).createVM()
		})
	}

	if utils.ShouldTestFeature("Test_getVM") {
		t.Run("Test_getVM", func(t *testing.T) {
			config.Child(t).getVM()
		})
	}

	if utils.ShouldTestFeature("Test_statusVM") {
		t.Run("Test_statusVM", func(t *testing.T) {
			config.Child(t).statusVM()
		})
	}

	if utils.ShouldTestFeature("Test_powerOnVM") {
		t.Run("Test_powerOnVM", func(t *testing.T) {
			config.Child(t).powerOnVM()
		})
	}

	if utils.ShouldTestFeature("Test_waitForPowered") {
		t.Run("Test_waitForPowered", func(t *testing.T) {
			config.Child(t).waitForPowered()
		})
	}

	if utils.ShouldTestFeature("Test_waitForIP") {
		t.Run("Test_waitForIP", func(t *testing.T) {
			config.Child(t).waitForIP()
		})
	}

	if utils.ShouldTestFeature("Test_powerOffVM") {
		t.Run("Test_powerOffVM", func(t *testing.T) {
			config.Child(t).powerOffVM()
		})
	}

	if utils.ShouldTestFeature("Test_shutdownGuest") {
		t.Run("Test_shutdownGuest", func(t *testing.T) {
			config.Child(t).shutdownVM()
		})
	}

	if utils.ShouldTestFeature("Test_deleteVM") {
		t.Run("Test_deleteVM", func(t *testing.T) {
			config.Child(t).deleteVM()
		})
	}
}

func Test_getVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_getVM") {
		config := loadFromJson(t)

		config.getVM()
	}
}

func Test_createVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_createVM") {
		config := loadFromJson(t)

		config.createVM()
	}
}

func Test_statusVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_statusVM") {
		config := loadFromJson(t)

		config.statusVM()
	}
}

func Test_waitForPowered(t *testing.T) {
	if utils.ShouldTestFeature("Test_waitForPowered") {
		config := loadFromJson(t)

		config.waitForPowered()
	}
}

func Test_waitForIP(t *testing.T) {
	if utils.ShouldTestFeature("Test_waitForIP") {
		config := loadFromJson(t)

		config.waitForIP()
	}
}

func Test_powerOnVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_powerOnVM") {
		config := loadFromJson(t)

		config.powerOnVM()
	}
}

func Test_powerOffVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_powerOffVM") {
		config := loadFromJson(t)

		config.powerOffVM()
	}
}

func Test_shutdownGuest(t *testing.T) {
	if utils.ShouldTestFeature("Test_shutdownGuest") {
		config := loadFromJson(t)

		config.shutdownVM()
	}
}

func Test_deleteVM(t *testing.T) {
	if utils.ShouldTestFeature("Test_deleteVM") {
		config := loadFromJson(t)

		config.deleteVM()
	}
}
