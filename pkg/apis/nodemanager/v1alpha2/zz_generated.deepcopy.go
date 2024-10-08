//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
Copyright 2024 Frédéric Boltz.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by deepcopy-gen. DO NOT EDIT.

package v1alpha2

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AwsManagedNodeNetwork) DeepCopyInto(out *AwsManagedNodeNetwork) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AwsManagedNodeNetwork.
func (in *AwsManagedNodeNetwork) DeepCopy() *AwsManagedNodeNetwork {
	if in == nil {
		return nil
	}
	out := new(AwsManagedNodeNetwork)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CloudStackManagedNodeNetwork) DeepCopyInto(out *CloudStackManagedNodeNetwork) {
	*out = *in
	out.CommonManagedNodeNetwork = in.CommonManagedNodeNetwork
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CloudStackManagedNodeNetwork.
func (in *CloudStackManagedNodeNetwork) DeepCopy() *CloudStackManagedNodeNetwork {
	if in == nil {
		return nil
	}
	out := new(CloudStackManagedNodeNetwork)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in CloudStackManagedNodeNetworks) DeepCopyInto(out *CloudStackManagedNodeNetworks) {
	{
		in := &in
		*out = make(CloudStackManagedNodeNetworks, len(*in))
		copy(*out, *in)
		return
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CloudStackManagedNodeNetworks.
func (in CloudStackManagedNodeNetworks) DeepCopy() CloudStackManagedNodeNetworks {
	if in == nil {
		return nil
	}
	out := new(CloudStackManagedNodeNetworks)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CommonManagedNodeNetwork) DeepCopyInto(out *CommonManagedNodeNetwork) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CommonManagedNodeNetwork.
func (in *CommonManagedNodeNetwork) DeepCopy() *CommonManagedNodeNetwork {
	if in == nil {
		return nil
	}
	out := new(CommonManagedNodeNetwork)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LxdManagedNodeNetwork) DeepCopyInto(out *LxdManagedNodeNetwork) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LxdManagedNodeNetwork.
func (in *LxdManagedNodeNetwork) DeepCopy() *LxdManagedNodeNetwork {
	if in == nil {
		return nil
	}
	out := new(LxdManagedNodeNetwork)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in LxdManagedNodeNetworks) DeepCopyInto(out *LxdManagedNodeNetworks) {
	{
		in := &in
		*out = make(LxdManagedNodeNetworks, len(*in))
		copy(*out, *in)
		return
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LxdManagedNodeNetworks.
func (in LxdManagedNodeNetworks) DeepCopy() LxdManagedNodeNetworks {
	if in == nil {
		return nil
	}
	out := new(LxdManagedNodeNetworks)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManagedNetworkConfig) DeepCopyInto(out *ManagedNetworkConfig) {
	*out = *in
	if in.OpenStack != nil {
		in, out := &in.OpenStack, &out.OpenStack
		*out = make(OpenStackManagedNodeNetworks, len(*in))
		copy(*out, *in)
	}
	if in.CloudStack != nil {
		in, out := &in.CloudStack, &out.CloudStack
		*out = make(CloudStackManagedNodeNetworks, len(*in))
		copy(*out, *in)
	}
	if in.VMWare != nil {
		in, out := &in.VMWare, &out.VMWare
		*out = make(VMWareManagedNodeNetworks, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Multipass != nil {
		in, out := &in.Multipass, &out.Multipass
		*out = make(MultipassManagedNodeNetworks, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Lxd != nil {
		in, out := &in.Lxd, &out.Lxd
		*out = make(LxdManagedNodeNetworks, len(*in))
		copy(*out, *in)
	}
	if in.ENI != nil {
		in, out := &in.ENI, &out.ENI
		*out = new(AwsManagedNodeNetwork)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManagedNetworkConfig.
func (in *ManagedNetworkConfig) DeepCopy() *ManagedNetworkConfig {
	if in == nil {
		return nil
	}
	out := new(ManagedNetworkConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManagedNode) DeepCopyInto(out *ManagedNode) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManagedNode.
func (in *ManagedNode) DeepCopy() *ManagedNode {
	if in == nil {
		return nil
	}
	out := new(ManagedNode)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ManagedNode) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManagedNodeList) DeepCopyInto(out *ManagedNodeList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ManagedNode, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManagedNodeList.
func (in *ManagedNodeList) DeepCopy() *ManagedNodeList {
	if in == nil {
		return nil
	}
	out := new(ManagedNodeList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ManagedNodeList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManagedNodeSpec) DeepCopyInto(out *ManagedNodeSpec) {
	*out = *in
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Annotations != nil {
		in, out := &in.Annotations, &out.Annotations
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	in.Networking.DeepCopyInto(&out.Networking)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManagedNodeSpec.
func (in *ManagedNodeSpec) DeepCopy() *ManagedNodeSpec {
	if in == nil {
		return nil
	}
	out := new(ManagedNodeSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ManagedNodeStatus) DeepCopyInto(out *ManagedNodeStatus) {
	*out = *in
	in.LastUpdateTime.DeepCopyInto(&out.LastUpdateTime)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ManagedNodeStatus.
func (in *ManagedNodeStatus) DeepCopy() *ManagedNodeStatus {
	if in == nil {
		return nil
	}
	out := new(ManagedNodeStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MultipassManagedNodeNetwork) DeepCopyInto(out *MultipassManagedNodeNetwork) {
	*out = *in
	out.CommonManagedNodeNetwork = in.CommonManagedNodeNetwork
	if in.UseRoutes != nil {
		in, out := &in.UseRoutes, &out.UseRoutes
		*out = new(bool)
		**out = **in
	}
	if in.Routes != nil {
		in, out := &in.Routes, &out.Routes
		*out = make([]NetworkRoutes, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MultipassManagedNodeNetwork.
func (in *MultipassManagedNodeNetwork) DeepCopy() *MultipassManagedNodeNetwork {
	if in == nil {
		return nil
	}
	out := new(MultipassManagedNodeNetwork)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in MultipassManagedNodeNetworks) DeepCopyInto(out *MultipassManagedNodeNetworks) {
	{
		in := &in
		*out = make(MultipassManagedNodeNetworks, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
		return
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MultipassManagedNodeNetworks.
func (in MultipassManagedNodeNetworks) DeepCopy() MultipassManagedNodeNetworks {
	if in == nil {
		return nil
	}
	out := new(MultipassManagedNodeNetworks)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NetworkRoutes) DeepCopyInto(out *NetworkRoutes) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NetworkRoutes.
func (in *NetworkRoutes) DeepCopy() *NetworkRoutes {
	if in == nil {
		return nil
	}
	out := new(NetworkRoutes)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *OpenStackManagedNodeNetwork) DeepCopyInto(out *OpenStackManagedNodeNetwork) {
	*out = *in
	out.CommonManagedNodeNetwork = in.CommonManagedNodeNetwork
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OpenStackManagedNodeNetwork.
func (in *OpenStackManagedNodeNetwork) DeepCopy() *OpenStackManagedNodeNetwork {
	if in == nil {
		return nil
	}
	out := new(OpenStackManagedNodeNetwork)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in OpenStackManagedNodeNetworks) DeepCopyInto(out *OpenStackManagedNodeNetworks) {
	{
		in := &in
		*out = make(OpenStackManagedNodeNetworks, len(*in))
		copy(*out, *in)
		return
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OpenStackManagedNodeNetworks.
func (in OpenStackManagedNodeNetworks) DeepCopy() OpenStackManagedNodeNetworks {
	if in == nil {
		return nil
	}
	out := new(OpenStackManagedNodeNetworks)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VMWareManagedNodeNetwork) DeepCopyInto(out *VMWareManagedNodeNetwork) {
	*out = *in
	out.CommonManagedNodeNetwork = in.CommonManagedNodeNetwork
	if in.UseRoutes != nil {
		in, out := &in.UseRoutes, &out.UseRoutes
		*out = new(bool)
		**out = **in
	}
	if in.Routes != nil {
		in, out := &in.Routes, &out.Routes
		*out = make([]NetworkRoutes, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VMWareManagedNodeNetwork.
func (in *VMWareManagedNodeNetwork) DeepCopy() *VMWareManagedNodeNetwork {
	if in == nil {
		return nil
	}
	out := new(VMWareManagedNodeNetwork)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in VMWareManagedNodeNetworks) DeepCopyInto(out *VMWareManagedNodeNetworks) {
	{
		in := &in
		*out = make(VMWareManagedNodeNetworks, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
		return
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VMWareManagedNodeNetworks.
func (in VMWareManagedNodeNetworks) DeepCopy() VMWareManagedNodeNetworks {
	if in == nil {
		return nil
	}
	out := new(VMWareManagedNodeNetworks)
	in.DeepCopyInto(out)
	return *out
}
