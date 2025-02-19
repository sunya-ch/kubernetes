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
	"fmt"

	resourceapi "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// AllocatedCapacity define a quantity set which is updatable.
// This field is used for aggregating allocated capacity,
// and for calculating consumability.
type AllocatedCapacity map[resourceapi.QualifiedName]*resource.Quantity

// AllocatedCapacityCollection collects a set of AllocatedCapacity
// for each shared device.
type AllocatedCapacityCollection map[DeviceID]AllocatedCapacity

func (c AllocatedCapacityCollection) Clone() AllocatedCapacityCollection {
	clone := make(AllocatedCapacityCollection)
	for deviceID, share := range c {
		clone[deviceID] = share.Clone()
	}
	return clone
}

func (c AllocatedCapacityCollection) Insert(new SharedDeviceAllocation) {
	if _, found := c[new.DeviceID]; found {
		c[new.DeviceID].Add(new.AllocatedCapacity)
	} else {
		c[new.DeviceID] = new.AllocatedCapacity.Clone()
	}
}

func (c AllocatedCapacityCollection) Remove(new SharedDeviceAllocation) {
	if _, found := c[new.DeviceID]; found {
		c[new.DeviceID].Sub(new.AllocatedCapacity)
		if c[new.DeviceID].HasNoShare() {
			delete(c, new.DeviceID)
		}
	}
}

func NewAllocatedShare() AllocatedCapacity {
	return make(AllocatedCapacity)
}

// GetCapacityAllocationFromRequest returns allocated resource,
// according to claim request and defined capacity.
func GetCapacityAllocationFromRequest(request *resourceapi.DeviceRequest,
	consumableCapacity map[resourceapi.QualifiedName]resourceapi.DeviceCapacity) map[resourceapi.QualifiedName]resource.Quantity {
	requestedCapacity := make(map[resourceapi.QualifiedName]resource.Quantity)

	for name, requestVal := range request.Capacity.Requests {
		requestedCapacity[name] = requestVal
	}
	// for name, limitVal := range request.Capacity.Limits {
	// 	requestedCapacity[name] = limitVal
	// }

	return requestedCapacity
}

// Copy makes a copy of AllocatedShare
func (s AllocatedCapacity) Clone() AllocatedCapacity {
	clone := make(AllocatedCapacity)
	for name, quantity := range s {
		q := quantity.DeepCopy()
		clone[name] = &q
	}
	return clone
}

// Add adds quantity to corresponding consumable capacity.
// add new entry if no corresponding consumable capacity found.
func (s AllocatedCapacity) Add(addedShare AllocatedCapacity) {
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
func (s AllocatedCapacity) Sub(substractedShare AllocatedCapacity) {
	for name, quantity := range substractedShare {
		if _, found := s[name]; found {
			s[name].Sub(*quantity)
		}
	}
}

// CmpRequestOverCapacity checks whether the new request can be added within the given capacity.
func (s AllocatedCapacity) CmpRequestOverCapacity(request *resourceapi.DeviceRequest,
	capacity map[resourceapi.QualifiedName]resourceapi.DeviceCapacity) (bool, error) {
	clone := s.Clone()
	requestedCapacity := GetCapacityAllocationFromRequest(request, capacity)
	for name := range requestedCapacity {
		if _, found := capacity[name]; !found {
			return false, fmt.Errorf("%s has not been defined in capacitiy", name)
		}
	}
	for name, cap := range capacity {
		requestedVal, requestedFound := requestedCapacity[name]
		if !requestedFound {
			if isRequiredConsumableCapacity(cap) {
				return false, fmt.Errorf("require %s in the resource request", name)
			}
			// does not request this resource, continue
			continue
		}
		if isConsumableCapacity(cap) {
			if violateConstraints(requestedVal, cap.ConsumeConstraint) {
				return false, nil
			}
			_, allocatedFound := clone[name]
			if !allocatedFound {
				clone[name] = &requestedVal
			} else {
				clone[name].Add(requestedVal)
			}
			if clone[name].Cmp(cap.Value) > 0 {
				return false, nil
			}
		} else {
			if requestedVal.Cmp(cap.Value) > 0 {
				return false, nil
			}
		}
	}
	return true, nil
}

// HasNoShare return true if all quantity is zero.
func (s AllocatedCapacity) HasNoShare() bool {
	for _, quantity := range s {
		if !quantity.IsZero() {
			return false
		}
	}
	return true
}

// SharedDeviceAllocation defines resource allocation results of the shared device.
type SharedDeviceAllocation struct {
	DeviceID
	AllocatedCapacity
}

func NewSharedDeviceAllocation(deviceID DeviceID, consumedCapacity map[resourceapi.QualifiedName]resource.Quantity) SharedDeviceAllocation {
	allocatedCapacity := make(AllocatedCapacity)
	for name, quantity := range consumedCapacity {
		allocatedCapacity[name] = &quantity
	}
	return SharedDeviceAllocation{
		DeviceID:          deviceID,
		AllocatedCapacity: allocatedCapacity,
	}
}

func (a SharedDeviceAllocation) Clone() SharedDeviceAllocation {
	return SharedDeviceAllocation{
		DeviceID:          a.DeviceID,
		AllocatedCapacity: a.AllocatedCapacity.Clone(),
	}
}

func (a SharedDeviceAllocation) String() string {
	return a.DeviceID.String()
}

func isConsumableCapacity(cap resourceapi.DeviceCapacity) bool {
	return cap.Consumable != nil && *cap.Consumable == true
}

func isRequiredConsumableCapacity(cap resourceapi.DeviceCapacity) bool {
	return isConsumableCapacity(cap) && cap.ConsumeConstraint != nil &&
		cap.ConsumeConstraint.Required != nil && *cap.ConsumeConstraint.Required
}

// violateConstraints checks whether the request violate the consume constraints.
func violateConstraints(requestedVal resource.Quantity, constraints *resourceapi.ConsumeConstraint) bool {
	if constraints == nil {
		return false
	}
	if constraints.ConsumeRange != nil {
		if constraints.ConsumeRange.Maximum != nil &&
			requestedVal.Cmp(*constraints.Maximum) > 0 {
			return true
		}
		if constraints.ConsumeRange.Minimum != nil &&
			requestedVal.Cmp(*constraints.Minimum) < 0 {
			return true
		}
		return false
	}
	if constraints.Set != nil && len(*constraints.Set) > 0 {
		for _, validVal := range *constraints.Set {
			if requestedVal.Cmp(validVal) == 0 {
				return false
			}
		}
		return true
	}
	return false
}
