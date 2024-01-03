package multipass

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	glog "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

const (
	multipassCommandLine      = "multipass"
	errCloudInitWriteError    = "can't write cloud-init, reason: %v"
	errCloudInitMarshallError = "can't marshall cloud-init, reason: %v"
	errTempFile               = "can't create temp file, reason: %v"
)

// Configuration declares multipass connection info
type Configuration struct {
	Timeout time.Duration `json:"timeout"`
}

type CreateInput struct {
	*providers.InstanceCreateInput
}
type multipassWrapper struct {
	Configuration
}

type multipassHandler struct {
	*multipassWrapper
	instanceType string
	instanceName string
	nodeIndex    int
}

type VMStatus struct {
	CPUCount string `json:"cpu_count"`
	Disks    struct {
		Sda1 struct {
			Total string `json:"total"`
			Used  string `json:"used"`
		} `json:"sda1"`
	} `json:"disks"`
	ImageHash    string   `json:"image_hash"`
	ImageRelease string   `json:"image_release"`
	Ipv4         []string `json:"ipv4"`
	Load         []int    `json:"load"`
	Memory       struct {
		Total int `json:"total"`
		Used  int `json:"used"`
	} `json:"memory"`
	Mounts struct {
		Home struct {
			GidMappings []string `json:"gid_mappings"`
			SourcePath  string   `json:"source_path"`
			UIDMappings []string `json:"uid_mappings"`
		} `json:"Home"`
	} `json:"mounts"`
	Release string `json:"release"`
	State   string `json:"state"`
}

type MultipassVMInfos struct {
	Errors []any               `json:"errors"`
	Info   map[string]VMStatus `json:"info"`
}

func NewMultipassProviderConfiguration(fileName string) (providers.ProviderConfiguration, error) {
	var wrapper multipassWrapper

	if err := providers.LoadConfig(fileName, &wrapper.Configuration); err != nil {
		glog.Errorf("Failed to open file:%s, error:%v", fileName, err)

		return nil, err
	}

	return &wrapper, nil
}

func (status *VMStatus) Address() string {
	if len(status.Ipv4) > 0 {
		return status.Ipv4[0]
	}
	return ""
}

func (status *VMStatus) Powered() bool {
	return strings.ToUpper(status.State) == "RUNNING"
}

func (wrapper *multipassWrapper) shell(args ...string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	glog.Debugf("Shell:%v", args)

	cmd := exec.Command(args[0], args[1:]...)

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stderr.String(), fmt.Errorf("%s, %s", err.Error(), strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), nil
}

func (wrapper *multipassWrapper) AttachInstance(instanceName string, nodeIndex int) (providers.ProviderHandler, error) {
	return &multipassHandler{
		multipassWrapper: wrapper,
		instanceName:     instanceName,
		nodeIndex:        nodeIndex,
	}, nil
}

func (wrapper *multipassWrapper) CreateInstance(instanceName, instanceType string, nodeIndex int) (providers.ProviderHandler, error) {
	return &multipassHandler{
		multipassWrapper: wrapper,
		instanceType:     instanceType,
		instanceName:     instanceName,
		nodeIndex:        nodeIndex,
	}, nil
}

func (wrapper *multipassWrapper) GetAvailableGpuTypes() map[string]string {
	return map[string]string{}
}

func (wrapper *multipassWrapper) InstanceExists(name string) bool {
	_, err := wrapper.shell(multipassCommandLine, "info", name)

	return err == nil
}

func (wrapper *multipassWrapper) UUID(name string) (string, error) {
	if wrapper.InstanceExists(name) {
		return name, nil
	} else {
		return name, fmt.Errorf("instance: %s  doesn't exists", name)
	}
}

func (wrapper *multipassWrapper) powerOn(instanceName string) error {
	if out, err := wrapper.shell(multipassCommandLine, "start", instanceName); err != nil {
		glog.Errorf("unable to start VM: %s, %s, reason: %v", instanceName, out, err)
		return err
	}

	return nil
}

func (wrapper *multipassWrapper) powerOff(instanceName string) error {
	if out, err := wrapper.shell(multipassCommandLine, "stop", instanceName); err != nil {
		glog.Errorf("unable to stop VM: %s, %s, reason: %v", instanceName, out, err)
		return err
	}

	return nil
}

func (wrapper *multipassWrapper) delete(instanceName string) error {
	if out, err := wrapper.shell(multipassCommandLine, "delete", instanceName, "-p"); err != nil {
		glog.Errorf("unable to delete VM: %s, %s, reason: %v", instanceName, out, err)
		return err
	}

	return nil
}

func (wrapper *multipassWrapper) status(instanceName string) (providers.InstanceStatus, error) {
	if out, err := wrapper.shell(multipassCommandLine, "info", instanceName, "--format", "json"); err != nil {
		glog.Errorf("unable to get VM info: %s, %s, reason: %v", instanceName, out, err)
		return nil, err
	} else {
		var infos MultipassVMInfos

		if err = json.NewDecoder(strings.NewReader(out)).Decode(&infos); err != nil {
			glog.Errorf("unable to decode info: %s, %s, reason: %v", instanceName, out, err)

			return nil, err
		} else if vminfo, found := infos.Info[instanceName]; found {
			return &vminfo, nil
		} else {
			return nil, fmt.Errorf("unable to find VM info: %s, in response: %s", instanceName, out)
		}
	}

}

func (wrapper *multipassWrapper) waitForIP(instanceName string, callback providers.CallbackWaitSSHReady) (string, error) {
	address := ""

	if err := context.PollImmediate(time.Second, wrapper.Timeout*time.Second, func() (bool, error) {
		if status, err := wrapper.status(instanceName); err != nil {
			return false, err
		} else if status.Powered() && len(status.Address()) > 0 {
			glog.Debugf("WaitForIP: instance %s, using IP:%s", instanceName, status.Address())

			if err = callback.WaitSSHReady(instanceName, status.Address()); err != nil {
				return false, err
			}
			address = status.Address()
			return true, nil
		} else {
			return false, nil
		}
	}); err != nil {
		return "", err
	}

	return address, nil
}

func (wrapper *multipassWrapper) waitForPowered(instanceName string) (err error) {
	return context.PollImmediate(time.Second, wrapper.Timeout*time.Second, func() (bool, error) {
		if status, err := wrapper.status(instanceName); err != nil {
			return false, err
		} else if status.Powered() {
			return true, nil
		} else {
			return false, nil
		}
	})
}

func (handler *multipassHandler) GetTimeout() time.Duration {
	return handler.Timeout
}

func (handler *multipassHandler) ConfigureNetwork(network v1alpha1.ManagedNetworkConfig) {
	// Nothing
}

func (handler *multipassHandler) RetrieveNetworkInfos() error {
	return nil
}

func (handler *multipassHandler) UpdateMacAddressTable() error {
	return nil
}

func (handler *multipassHandler) GenerateProviderID() string {
	return fmt.Sprintf("multipass://%s", handler.instanceName)
}

func (handler *multipassHandler) GetTopologyLabels() map[string]string {
	return map[string]string{}
}

func (handler *multipassHandler) writeCloudFile(input *providers.InstanceCreateInput) (*os.File, error) {
	var cloudInitFile *os.File
	var err error
	var b []byte

	if input.CloudInit != nil {
		fName := fmt.Sprintf("%s/cloud-init-%s.yaml", os.TempDir(), handler.instanceName)
		cloudInitFile, err = os.Create(fName)

		glog.Infof("Create cloud file: %s", fName)

		if err == nil {
			if b, err = yaml.Marshal(input.CloudInit); err == nil {
				if _, err = cloudInitFile.Write(b); err != nil {
					err = fmt.Errorf(errCloudInitWriteError, err)
				}
			} else {
				err = fmt.Errorf(errCloudInitMarshallError, err)
			}
		} else {
			err = fmt.Errorf(errTempFile, err)
		}

		if err != nil {
			os.Remove(fName)
		}
	}

	return cloudInitFile, err
}

func (handler *multipassHandler) InstanceCreate(input *providers.InstanceCreateInput) (string, error) {
	if cloudInitFile, err := handler.writeCloudFile(input); err != nil {
		return "", err
	} else {
		args := []string{
			multipassCommandLine,
			"launch",
			"--name",
			handler.instanceName,
		}

		if input.Machine.Memory > 0 {
			args = append(args, fmt.Sprintf("--mem=%dM", input.Machine.Memory))
		}

		if input.Machine.Vcpu > 0 {
			args = append(args, fmt.Sprintf("--cpus=%d", input.Machine.Vcpu))
		}

		if input.DiskSize > 0 {
			args = append(args, fmt.Sprintf("--disk=%dM", input.DiskSize))
		}

		if cloudInitFile != nil {
			args = append(args, fmt.Sprintf("--cloud-init=%s", cloudInitFile.Name()))
		}

		if len(handler.instanceType) > 0 {
			args = append(args, handler.instanceType)
		}

		if out, err := handler.shell(args...); err != nil {
			glog.Errorf("unalble to create VM: %s, output: %s, reason: %v", handler.instanceName, out, err)
			return "", err
		}

		return handler.instanceName, nil
	}
}

func (handler *multipassHandler) InstanceWaitReady(callback providers.CallbackWaitSSHReady) (string, error) {
	return handler.waitForIP(handler.instanceName, callback)
}

func (handler *multipassHandler) InstanceID() (string, error) {
	return handler.instanceName, nil
}

func (handler *multipassHandler) InstanceAutoStart() error {
	return nil
}

func (handler *multipassHandler) InstancePowerOn() error {
	return handler.powerOn(handler.instanceName)
}

func (handler *multipassHandler) InstancePowerOff() error {
	return handler.powerOff(handler.instanceName)
}

func (handler *multipassHandler) InstanceShutdownGuest() error {
	return handler.powerOff(handler.instanceName)

}

func (handler *multipassHandler) InstanceDelete() error {
	return handler.delete(handler.instanceName)
}

func (handler *multipassHandler) InstanceStatus() (providers.InstanceStatus, error) {
	return handler.status(handler.instanceName)
}

func (handler *multipassHandler) InstanceWaitForPowered() error {
	return handler.waitForPowered(handler.instanceName)
}

func (handler *multipassHandler) InstanceWaitForToolsRunning() (bool, error) {
	return true, nil
}

func (handler *multipassHandler) InstanceMaxPods(desiredMaxPods int) (int, error) {
	if desiredMaxPods == 0 {
		desiredMaxPods = 110
	}

	return desiredMaxPods, nil
}

func (handler *multipassHandler) RegisterDNS(address string) error {
	return nil
}

func (handler *multipassHandler) UnregisterDNS(address string) error {
	return nil
}

func (handler *multipassHandler) UUID(name string) (string, error) {
	return name, nil
}
