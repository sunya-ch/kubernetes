/*
Copyright 2024 The Kubernetes Authors.

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

package structured

import (
	resourceapi "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/structured/internal"
)

type SharedDeviceID = internal.SharedDeviceID
type ConsumedCapacityCollection = internal.ConsumedCapacityCollection
type DeviceConsumedCapacity = internal.DeviceConsumedCapacity
type ConsumedCapacity = internal.ConsumedCapacity
type AllocatedState = internal.AllocatedState

func MakeSharedDeviceID(deviceID DeviceID, shareID *types.UID) SharedDeviceID {
	return internal.MakeSharedDeviceID(deviceID, shareID)
}

func NewConsumedCapacityCollection() ConsumedCapacityCollection {
	return internal.NewConsumedCapacityCollection()
}

func NewDeviceConsumedCapacity(deviceID DeviceID,
	consumedCapacity map[resourceapi.QualifiedName]resource.Quantity) DeviceConsumedCapacity {
	return internal.NewDeviceConsumedCapacity(deviceID, consumedCapacity)
}
