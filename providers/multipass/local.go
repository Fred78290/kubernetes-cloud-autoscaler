package multipass

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
	glog "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type hostMultipassWrapper struct {
	baseMultipassWrapper
}

func (wrapper *hostMultipassWrapper) shell(args ...string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	glog.Debugf("Shell: %v", args)

	cmd := exec.Command(args[0], args[1:]...)

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stderr.String(), fmt.Errorf("%s, %s", err.Error(), strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), nil
}

func (wrapper *hostMultipassWrapper) AttachInstance(instanceName string, controlPlane bool, nodeIndex int) (providers.ProviderHandler, error) {
	return &multipassHandler{
		multipassWrapper: wrapper,
		network:          wrapper.Network.Clone(controlPlane, nodeIndex),
		instanceName:     instanceName,
		controlPlane:     controlPlane,
		nodeIndex:        nodeIndex,
	}, nil
}

func (wrapper *hostMultipassWrapper) CreateInstance(instanceName, instanceType string, controlPlane bool, nodeIndex int) (providers.ProviderHandler, error) {
	return &multipassHandler{
		multipassWrapper: wrapper,
		network:          wrapper.Network.Clone(controlPlane, nodeIndex),
		instanceType:     instanceType,
		instanceName:     instanceName,
		controlPlane:     controlPlane,
		nodeIndex:        nodeIndex,
	}, nil
}

func (wrapper *hostMultipassWrapper) GetAvailableGpuTypes() map[string]string {
	return map[string]string{}
}

func (wrapper *hostMultipassWrapper) InstanceExists(name string) bool {
	_, err := wrapper.shell(multipassCommandLine, "info", name)

	return err == nil
}

func (wrapper *hostMultipassWrapper) UUID(name string) (string, error) {
	if wrapper.InstanceExists(name) {
		return name, nil
	} else {
		return name, fmt.Errorf("instance: %s  doesn't exists", name)
	}
}

func (wrapper *hostMultipassWrapper) getConfiguration() *Configuration {
	return wrapper.Configuration
}

func (wrapper *hostMultipassWrapper) powerOn(instanceName string) error {
	if out, err := wrapper.shell(multipassCommandLine, "start", instanceName); err != nil {
		glog.Errorf("unable to start VM: %s, %s, reason: %v", instanceName, out, err)
		return err
	}

	return nil
}

func (wrapper *hostMultipassWrapper) powerOff(instanceName string) error {
	if out, err := wrapper.shell(multipassCommandLine, "stop", instanceName); err != nil {
		glog.Errorf("unable to stop VM: %s, %s, reason: %v", instanceName, out, err)
		return err
	}

	return nil
}

func (wrapper *hostMultipassWrapper) delete(instanceName string) error {
	if out, err := wrapper.shell(multipassCommandLine, "delete", instanceName, "-p"); err != nil {
		glog.Errorf("unable to delete VM: %s, %s, reason: %v", instanceName, out, err)
		return err
	}

	return nil
}

func (wrapper *hostMultipassWrapper) status(instanceName string) (providers.InstanceStatus, error) {
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

func (wrapper *hostMultipassWrapper) marshall(obj any) ([]byte, error) {
	var out bytes.Buffer

	fmt.Fprintln(&out, "#cloud-config")

	wr := yaml.NewEncoder(&out)

	if err := wr.Encode(obj); err == nil {
		wr.Close()

		return out.Bytes(), nil
	} else {
		return nil, err
	}
}

func (wrapper *hostMultipassWrapper) writeCloudFile(input *createInstanceInput) (*os.File, error) {
	tz, _ := time.Now().Zone()

	cloudInitInput := cloudinit.CloudInitInput{
		InstanceName: input.instanceName,
		DomainName:   input.network.Domain,
		UserName:     input.UserName,
		AuthKey:      input.AuthKey,
		TimeZone:     tz,
		AllowUpgrade: wrapper.AllowUpgrade,
		CloudInit:    input.CloudInit,
	}

	if input.network != nil && len(input.network.Interfaces) > 0 {
		cloudInitInput.Network = input.network.GetCloudInitNetwork(false)
	}

	if cloudInit, err := cloudInitInput.BuildUserData(input.netplanFile); err != nil {
		return nil, err
	} else {
		fName := fmt.Sprintf("%s/cloud-init-%s.yaml", desktopUtilityTempDirectory(), input.instanceName)

		if cloudInitFile, err := os.Create(fName); err != nil {
			return nil, fmt.Errorf(errTempFile, err)
		} else if b, err := wrapper.marshall(cloudInit); err != nil {
			os.Remove(fName)
			return nil, fmt.Errorf(errCloudInitMarshallError, err)
		} else if _, err = cloudInitFile.Write(b); err != nil {
			os.Remove(fName)
			return nil, fmt.Errorf(errCloudInitWriteError, err)
		} else {
			return cloudInitFile, err
		}
	}
}

func (wrapper *hostMultipassWrapper) create(input *createInstanceInput) (string, error) {
	if cloudInitFile, err := wrapper.writeCloudFile(input); err != nil {
		return "", err
	} else {
		args := []string{
			multipassCommandLine,
			"launch",
			"--name",
			input.instanceName,
		}

		if input.Machine.Memory > 0 {
			args = append(args, fmt.Sprintf("--mem=%dM", input.Machine.Memory))
		}

		if input.Machine.Vcpu > 0 {
			args = append(args, fmt.Sprintf("--cpus=%d", input.Machine.Vcpu))
		}

		if input.Machine.DiskSize > 0 {
			args = append(args, fmt.Sprintf("--disk=%dM", input.Machine.DiskSize))
		}

		if cloudInitFile != nil {
			args = append(args, fmt.Sprintf("--cloud-init=%s", cloudInitFile.Name()))
		}

		if wrapper.baseMultipassWrapper.Network != nil {
			for _, network := range wrapper.baseMultipassWrapper.Network.Interfaces {
				if network.Enabled && !network.Existing {
					var sb strings.Builder

					mode := network.ConnectionType
					sb.WriteString(fmt.Sprintf("name=%s", network.NetworkName))

					if !network.DHCP {
						mode = "manual"
					}

					mac := network.GetMacAddress()
					if len(mac) > 0 {
						sb.WriteString(fmt.Sprintf(",mac=%s", mac))
					}

					if strings.ToLower(mode) == "manual" {
						sb.WriteString(fmt.Sprintf(",mode=%s", network.ConnectionType))
					}

					args = append(args, "--network", sb.String())
				}
			}
		}

		for _, mount := range wrapper.baseMultipassWrapper.Mounts {
			args = append(args, fmt.Sprintf("--mount=%s:%s", mount.LocalPath, mount.InstancePath))
		}

		if len(input.instanceType) > 0 {
			args = append(args, input.instanceType)
		}

		if out, err := wrapper.shell(args...); err != nil {
			glog.Errorf("unalble to create VM: %s, output: %s, reason: %v", input.instanceName, out, err)
			return "", err
		}

		return input.instanceName, nil
	}
}
