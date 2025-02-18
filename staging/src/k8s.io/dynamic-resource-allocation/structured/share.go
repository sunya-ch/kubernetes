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
	"errors"
	"fmt"

	resourceapi "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type AllocatedShareCollection map[DeviceID]AllocatedShare

func (c AllocatedShareCollection) Clone() AllocatedShareCollection {
	clone := make(AllocatedShareCollection)
	for deviceID, share := range c {
		clone[deviceID] = share.Clone()
	}
	return clone
}

func (c AllocatedShareCollection) Insert(new AllocatedSharedDevice) {
	if _, found := c[new.DeviceID]; found {
		c[new.DeviceID].Add(new.AllocatedShare)
	} else {
		c[new.DeviceID] = new.AllocatedShare.Clone()
	}
}

func (c AllocatedShareCollection) Remove(new AllocatedSharedDevice) {
	if _, found := c[new.DeviceID]; found {
		c[new.DeviceID].Sub(new.AllocatedShare)
		if c[new.DeviceID].HasNoShare() {
			delete(c, new.DeviceID)
		}
	}
}

type AllocatedShare map[resourceapi.QualifiedName]*resource.Quantity

func NewAllocatedShare() AllocatedShare {
	return make(AllocatedShare)
}

// GetShareFromRequest returns resource share to be allocated,
// according to claim request and defined consumable capacity.
func GetShareFromRequest(request *resourceapi.DeviceRequest,
	consumableCapacity map[resourceapi.QualifiedName]resourceapi.DeviceConsumableCapacity) (
	map[resourceapi.QualifiedName]resource.Quantity, error) {
	requestedResource := make(map[resourceapi.QualifiedName]resource.Quantity)
	if IsFullDeviceRequest(*request) {
		for name, consumableCapacity := range consumableCapacity {
			if consumableCapacity.InfinityResource {
				requestedResource[name] = resource.MustParse("1")
				continue
			}
			if consumableCapacity.Value.IsZero() {
				return nil, fmt.Errorf("zero capacity on non-infinity attribute")
			}
			requestedResource[name] = consumableCapacity.Value
		}
	} else {
		for name, value := range request.Resources.Requests {
			requestedResource[name] = value
		}
	}
	return requestedResource, nil
}

// Copy makes a copy of AllocatedShare
func (s AllocatedShare) Clone() AllocatedShare {
	clone := make(AllocatedShare)
	for name, quantity := range s {
		q := quantity.DeepCopy()
		clone[name] = &q
	}
	return clone
}

// Add adds quantity to corresponding consumable capacity.
// add new entry if no corresponding consumable capacity found.
func (s AllocatedShare) Add(addedShare AllocatedShare) {
	for name, quantity := range addedShare {
		if _, found := s[name]; found {
			s[name].Add(*quantity)
		} else {
			s[name] = quantity
		}
	}
}

// Sub subtracts quantity
// ignore if no corresponding consumable capacity found.
func (s AllocatedShare) Sub(substractedShare AllocatedShare) {
	for name, quantity := range substractedShare {
		if _, found := s[name]; found {
			s[name].Sub(*quantity)
		}
	}
}

// IsConsumable checks whether the new request can be added given the consumable capacity.
func (s AllocatedShare) IsConsumable(requestedResources map[resourceapi.QualifiedName]resource.Quantity,
	consumableCapacity map[resourceapi.QualifiedName]resourceapi.DeviceConsumableCapacity) (bool, error) {
	clone := s.Clone()
	if requestedResources == nil {
		return false, errors.New("nil resource request.")
	}
	for name := range requestedResources {
		if _, found := consumableCapacity[name]; !found {
			return false, fmt.Errorf("%s has not been defined in consumable capacitiy", name)
		}
	}
	for name, consumableCapacity := range consumableCapacity {
		if consumableCapacity.InfinityResource {
			continue
		}
		if consumableCapacity.Value.IsZero() {
			return false, errors.New("consumable capacity is zero.")
		}
		requestedVal, requestedFound := requestedResources[name]
		if !requestedFound {
			// does not request this resource, continue
			continue
		}
		_, allocatedFound := clone[name]
		if !allocatedFound {
			clone[name] = &requestedVal
		} else {
			clone[name].Add(requestedVal)
		}
		if clone[name].Cmp(consumableCapacity.Value) > 0 {
			return false, nil
		}
	}
	return true, nil
}

// HasNoShare return true if all quantity is zero.
func (s AllocatedShare) HasNoShare() bool {
	for _, quantity := range s {
		if !quantity.IsZero() {
			return false
		}
	}
	return true
}

func NewAllocatedSharedDevice(deviceID DeviceID, requestedResource map[resourceapi.QualifiedName]resource.Quantity) AllocatedSharedDevice {
	allocatedShare := make(AllocatedShare)
	for name, quantity := range requestedResource {
		allocatedShare[name] = &quantity
	}
	return AllocatedSharedDevice{
		DeviceID:       deviceID,
		AllocatedShare: allocatedShare,
	}
}

type AllocatedSharedDevice struct {
	DeviceID
	AllocatedShare
}

func (a AllocatedSharedDevice) Clone() AllocatedSharedDevice {
	return AllocatedSharedDevice{
		DeviceID:       a.DeviceID,
		AllocatedShare: a.AllocatedShare.Clone(),
	}
}

func (a AllocatedSharedDevice) String() string {
	return a.DeviceID.String()
}

func IsFullDeviceRequest(request resourceapi.DeviceRequest) bool {
	return request.Resources == nil || request.Resources.All || request.Resources.Requests == nil
}
