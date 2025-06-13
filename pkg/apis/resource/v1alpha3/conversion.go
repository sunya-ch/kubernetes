/*
Copyright 2022 The Kubernetes Authors.

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

package v1alpha3

import (
	"k8s.io/apimachinery/pkg/runtime"
)

func addConversionFuncs(scheme *runtime.Scheme) error {
	return nil
}

func Convert_resource_DeviceSubRequest_To_v1alpha3_DeviceSubRequest(in *resourceapi.DeviceSubRequest, out *resourcev1alpha3.DeviceSubRequest, s conversion.Scope) error {
	out.Name = in.Name
	out.DeviceClassName = in.DeviceClassName
	out.Selectors = *(*[]resourcev1alpha3.DeviceSelector)(unsafe.Pointer(&in.Selectors))
	out.AllocationMode = resourcev1alpha3.DeviceAllocationMode(in.AllocationMode)
	out.Count = in.Count
	out.Tolerations = *(*[]resourcev1alpha3.DeviceToleration)(unsafe.Pointer(&in.Tolerations))
	return nil
}

func Convert_resource_DeviceRequestAllocationResult_To_v1alpha3_DeviceRequestAllocationResult(in *resourceapi.DeviceRequestAllocationResult, out *resourcev1alpha3.DeviceRequestAllocationResult, s conversion.Scope) error {
	out.Request = in.Request
	out.Driver = in.Driver
	out.Pool = in.Pool
	out.Device = in.Device
	out.AdminAccess = (*bool)(unsafe.Pointer(in.AdminAccess))
	out.Tolerations = *(*[]resourcev1alpha3.DeviceToleration)(unsafe.Pointer(&in.Tolerations))
	out.ShareID = (*string)(unsafe.Pointer(in.ShareID))
	return nil
}
