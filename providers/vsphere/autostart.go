package vsphere

import (
	"github.com/Fred78290/kubernetes-cloud-autoscaler/context"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

type HostAutoStartManager struct {
	mo.HostAutoStartManager
	Datacenter *Datacenter
}

func (h *HostAutoStartManager) NextStartOrder() (result int) {
	for _, info := range h.Config.PowerInfo {
		if info.StartOrder > int32(result) {
			result = int(info.StartOrder)
		}
	}

	return result + 1
}

func (h *HostAutoStartManager) SetAutoStart(ctx *context.Context, datastore, name string, startOrder, startDelay, stopDelay int) error {
	var err error
	var ds *Datastore
	var vm *VirtualMachine

	dc := h.Datacenter

	if startOrder <= 0 {
		startOrder = -1
	}

	if stopDelay <= 0 {
		stopDelay = -1
	}

	if startOrder <= 0 {
		startOrder = h.NextStartOrder()
	}

	if ds, err = dc.GetDatastore(ctx, datastore); err == nil {
		if vm, err = ds.VirtualMachine(ctx, name); err == nil {
			powerInfo := []types.AutoStartPowerInfo{{
				Key:              vm.Ref,
				StartOrder:       int32(startOrder),
				StartDelay:       int32(startDelay),
				WaitForHeartbeat: types.AutoStartWaitHeartbeatSettingSystemDefault,
				StartAction:      "powerOn",
				StopDelay:        int32(stopDelay),
				StopAction:       "systemDefault",
			}}

			req := types.ReconfigureAutostart{
				This: h.Self,
				Spec: types.HostAutoStartManagerConfig{
					PowerInfo: powerInfo,
				},
			}

			_, err = methods.ReconfigureAutostart(ctx, dc.VimClient(), &req)
		}
	}

	return err
}
