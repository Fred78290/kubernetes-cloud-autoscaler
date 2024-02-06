package multipass

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Fred78290/kubernetes-cloud-autoscaler/api"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/cloudinit"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/Fred78290/kubernetes-cloud-autoscaler/providers"
)

type remoteMultipassWrapper struct {
	baseMultipassWrapper
	client api.DesktopAutoscalerServiceClient
}

func (wrapper *remoteMultipassWrapper) AttachInstance(instanceName string, nodeIndex int) (providers.ProviderHandler, error) {
	return &multipassHandler{
		multipassWrapper: wrapper,
		network:          wrapper.Network.Clone(),
		instanceName:     instanceName,
		nodeIndex:        nodeIndex,
	}, nil
}

func (wrapper *remoteMultipassWrapper) CreateInstance(instanceName, instanceType string, nodeIndex int) (providers.ProviderHandler, error) {
	return &multipassHandler{
		multipassWrapper: wrapper,
		network:          wrapper.Network.Clone(),
		instanceType:     instanceType,
		instanceName:     instanceName,
		nodeIndex:        nodeIndex,
	}, nil
}

func (wrapper *remoteMultipassWrapper) InstanceExists(name string) bool {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	if response, err := wrapper.client.HostInfoInstance(ctx, &api.HostInstanceRequest{Driver: multipassCommandLine, InstanceName: name}); err != nil {
		return false
	} else if response.GetError() != nil {
		return false
	}

	return true
}

func (wrapper *remoteMultipassWrapper) UUID(name string) (string, error) {
	if wrapper.InstanceExists(name) {
		return name, nil
	} else {
		return name, fmt.Errorf("instance: %s  doesn't exists", name)
	}
}

func (wrapper *remoteMultipassWrapper) GetAvailableGpuTypes() map[string]string {
	return map[string]string{}
}

func (wrapper *remoteMultipassWrapper) getConfiguration() *Configuration {
	return wrapper.Configuration
}

func (wrapper *remoteMultipassWrapper) powerOn(instanceName string) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	if response, err := wrapper.client.HostStartInstance(ctx, &api.HostInstanceRequest{Driver: multipassCommandLine, InstanceName: instanceName}); err != nil {
		return err
	} else if response.GetError() != nil {
		return fmt.Errorf("powerOn failed. Code:%d, reason: %v", response.GetError().Code, response.GetError().Reason)
	}

	return nil
}

func (wrapper *remoteMultipassWrapper) powerOff(instanceName string) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	if response, err := wrapper.client.HostStopInstance(ctx, &api.HostInstanceRequest{Driver: multipassCommandLine, InstanceName: instanceName}); err != nil {
		return err
	} else if response.GetError() != nil {
		return fmt.Errorf("powerOff failed. Code:%d, reason: %v", response.GetError().Code, response.GetError().Reason)
	}

	return nil
}

func (wrapper *remoteMultipassWrapper) delete(instanceName string) error {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	if response, err := wrapper.client.HostDeleteInstance(ctx, &api.HostInstanceRequest{Driver: multipassCommandLine, InstanceName: instanceName}); err != nil {
		return err
	} else if response.GetError() != nil {
		return fmt.Errorf("delete failed. Code:%d, reason: %v", response.GetError().Code, response.GetError().Reason)
	}

	return nil
}

func (wrapper *remoteMultipassWrapper) status(instanceName string) (providers.InstanceStatus, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

	var infos MultipassVMInfos

	if response, err := wrapper.client.HostInfoInstance(ctx, &api.HostInstanceRequest{Driver: multipassCommandLine, InstanceName: instanceName}); err != nil {
		return nil, err
	} else if response.GetError() != nil {
		return nil, fmt.Errorf("status failed. Code:%d, reason: %v", response.GetError().Code, response.GetError().Reason)
	} else if err = json.NewDecoder(strings.NewReader(response.GetResult().Output)).Decode(&infos); err != nil {
		return nil, err
	} else if vminfo, found := infos.Info[instanceName]; found {
		return &vminfo, nil
	} else {
		return nil, fmt.Errorf("unable to find VM info: %s, in response: %s", instanceName, response.GetResult().Output)
	}
}

func (wrapper *remoteMultipassWrapper) create(input *createInstanceInput) (string, error) {
	ctx := context.NewContext(wrapper.Timeout)
	defer ctx.Cancel()

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
		cloudInitInput.Network = input.network.GetCloudInitNetwork(-1)
	}

	if cloudInitOut, err := cloudInitInput.BuildUserData(input.netplanFile); err != nil {
		return "", err
	} else {
		var buffer bytes.Buffer

		if err := json.NewEncoder(&buffer).Encode(cloudInitOut); err != nil {
			return "", nil
		}

		request := api.HostCreateInstanceRequest{
			Driver:       multipassCommandLine,
			InstanceName: input.instanceName,
			InstanceType: input.instanceType,
			Memory:       int32(input.Machine.Memory),
			Vcpu:         int32(input.Machine.Vcpu),
			DiskSize:     int32(input.Machine.DiskSize),
			CloudInit:    buffer.Bytes(),
		}

		if wrapper.baseMultipassWrapper.Network != nil {
			networks := make([]*api.HostNetworkInterface, 0, len(wrapper.baseMultipassWrapper.Network.Interfaces))

			for _, inf := range wrapper.baseMultipassWrapper.Network.Interfaces {
				if inf.Enabled && !inf.Existing {
					mode := inf.ConnectionType
					mac := inf.GetMacAddress(input.nodeIndex)

					if !inf.DHCP {
						mode = "manual"
					}

					networks = append(networks, &api.HostNetworkInterface{
						Name:       inf.NetworkName,
						Nic:        inf.NicName,
						Macaddress: mac,
						Mode:       mode,
					})
				}
			}

			request.Networks = networks
		}

		if len(wrapper.baseMultipassWrapper.Mounts) > 0 {
			mounts := make([]*api.HostMountPoint, 0, len(wrapper.baseMultipassWrapper.Mounts))

			for _, mount := range wrapper.baseMultipassWrapper.Mounts {
				mounts = append(mounts, &api.HostMountPoint{
					LocalPath:    mount.LocalPath,
					InstancePath: mount.InstancePath,
				})
			}

			request.Mounts = mounts
		}

		if response, err := wrapper.client.HostCreateInstance(ctx, &request); err != nil {
			return "", err
		} else if response.GetError() != nil {
			return "", fmt.Errorf("create failed. Code:%d, reason: %v", response.GetError().Code, response.GetError().Reason)
		} else {
			return response.GetResult().Output, nil
		}
	}
}
