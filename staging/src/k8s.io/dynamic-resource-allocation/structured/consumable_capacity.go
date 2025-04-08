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

	resourceapi "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// AllocatedCapacity define a quantity set which is updatable.
// This field is used for aggregating allocated capacity,
// and for calculating consumability.
type AllocatedCapacity map[resourceapi.QualifiedName]*resource.Quantity

// AllocatedCapacityCollection collects a set of AllocatedCapacity
// for each consumable capacity
type AllocatedCapacityCollection map[DeviceID]AllocatedCapacity

func NewAllocatedCapacity() AllocatedCapacity {
	return make(AllocatedCapacity)
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

// Empty return true if all quantity is zero.
func (s AllocatedCapacity) Empty() bool {
	for _, quantity := range s {
		if !quantity.IsZero() {
			return false
		}
	}
	return true
}

// CmpRequestOverCapacity checks whether the new request can be added within the given capacity.
func (s AllocatedCapacity) CmpRequestOverCapacity(request *resourceapi.DeviceRequest,
	capacity map[resourceapi.QualifiedName]resourceapi.DeviceCapacity) (bool, error) {
	if requestsContainNonExistCapacity(request, capacity) {
		return false, errors.New("some requested capacity has not been defined.")
	}
	clone := s.Clone()
	for name, cap := range capacity {
		var requestedValPtr *resource.Quantity
		if request.Capacity != nil && request.Capacity.Requests != nil {
			if requestedVal, requestedFound := request.Capacity.Requests[name]; requestedFound {
				requestedValPtr = &requestedVal
			}
		}
		if isConsumableCapacity(cap) {
			consumedCapacity := calculateConsumedCapacity(requestedValPtr, *cap.Consumable)
			if violateConstraints(*consumedCapacity, cap.Consumable) {
				return false, nil
			}
			_, allocatedFound := clone[name]
			if !allocatedFound {
				clone[name] = consumedCapacity
			} else {
				clone[name].Add(*consumedCapacity)
			}
			if clone[name].Cmp(cap.Value) > 0 {
				return false, nil
			}
		} else if requestedValPtr != nil {
			if (*requestedValPtr).Cmp(cap.Value) > 0 {
				return false, nil
			}
		}
	}
	return true, nil
}

// Clone makes a copy of AllocatedCapacity of each capacity.
func (c AllocatedCapacityCollection) Clone() AllocatedCapacityCollection {
	clone := make(AllocatedCapacityCollection)
	for deviceID, share := range c {
		clone[deviceID] = share.Clone()
	}
	return clone
}

// Insert adds a new allocated capacity to the collection.
func (c AllocatedCapacityCollection) Insert(cap DeviceAllocatedCapacity) {
	if _, found := c[cap.DeviceID]; found {
		c[cap.DeviceID].Add(cap.AllocatedCapacity)
	} else {
		c[cap.DeviceID] = cap.AllocatedCapacity.Clone()
	}
}

// Remove removes an allocated capacity from the collection.
func (c AllocatedCapacityCollection) Remove(cap DeviceAllocatedCapacity) {
	if _, found := c[cap.DeviceID]; found {
		c[cap.DeviceID].Sub(cap.AllocatedCapacity)
		if c[cap.DeviceID].Empty() {
			delete(c, cap.DeviceID)
		}
	}
}

// requestsNonExistCapacity returns true if requests contain non-exist capacity.
func requestsContainNonExistCapacity(request *resourceapi.DeviceRequest,
	capacity map[resourceapi.QualifiedName]resourceapi.DeviceCapacity) bool {
	if request.Capacity == nil || request.Capacity.Requests == nil {
		return false
	}
	for name := range request.Capacity.Requests {
		if _, found := capacity[name]; !found {
			return true
		}
	}
	return false
}

// DeviceAllocatedCapacity contains consumed capacity result within device allocation.
type DeviceAllocatedCapacity struct {
	DeviceID
	AllocatedCapacity
}

// NewDeviceAllocatedCapacity creates DeviceAllocatedCapacity instance from device ID and its consumed capacity.
func NewDeviceAllocatedCapacity(deviceID DeviceID, consumedCapacity map[resourceapi.QualifiedName]resource.Quantity) DeviceAllocatedCapacity {
	allocatedCapacity := make(AllocatedCapacity)
	for name, quantity := range consumedCapacity {
		allocatedCapacity[name] = &quantity
	}
	return DeviceAllocatedCapacity{
		DeviceID:          deviceID,
		AllocatedCapacity: allocatedCapacity,
	}
}

// Clone makes a copy of DeviceAllocatedCapacity.
func (a DeviceAllocatedCapacity) Clone() DeviceAllocatedCapacity {
	return DeviceAllocatedCapacity{
		DeviceID:          a.DeviceID,
		AllocatedCapacity: a.AllocatedCapacity.Clone(),
	}
}

// String returns formatted device ID.
func (a DeviceAllocatedCapacity) String() string {
	return a.DeviceID.String()
}

// isConsumableCapacity returns true if capacity has consumable spec defined.
func isConsumableCapacity(cap resourceapi.DeviceCapacity) bool {
	return cap.Consumable != nil
}

// violateConstraints checks whether the request violate the consume constraints.
func violateConstraints(requestedVal resource.Quantity, constraints *resourceapi.ConsumableSpec) bool {
	if constraints == nil {
		return false
	}
	if constraints.ConsumableRange != nil {
		if constraints.ConsumableRange.Maximum != nil &&
			requestedVal.Cmp(*constraints.Maximum) > 0 {
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

// calculateConsumedCapacity returns valid capacity to be consumed regarding the requested capacity and consumable spec.
func calculateConsumedCapacity(requestedVal *resource.Quantity, consumable resourceapi.ConsumableSpec) *resource.Quantity {
	if consumable.ConsumableRange != nil {
		if requestedVal == nil || requestedVal.Cmp(consumable.ConsumableRange.Minimum) < 0 {
			returnedVal := consumable.ConsumableRange.Minimum.DeepCopy()
			return &returnedVal
		}
	} else if consumable.ConsumableSet != nil {
		if requestedVal == nil {
			returnedVal := consumable.ConsumableSet.Default.DeepCopy()
			return &returnedVal
		}
	}
	return requestedVal
}

// GetConsumedCapacityFromRequest returns valid consumed capacity,
// according to claim request and defined capacity.
func GetConsumedCapacityFromRequest(request *resourceapi.DeviceRequest,
	consumableCapacity map[resourceapi.QualifiedName]resourceapi.DeviceCapacity) map[resourceapi.QualifiedName]resource.Quantity {
	consumedCapacities := make(map[resourceapi.QualifiedName]resource.Quantity)
	for name, cap := range consumableCapacity {
		if isConsumableCapacity(cap) {
			var requestedValPtr *resource.Quantity
			if request.Capacity != nil && request.Capacity.Requests != nil {
				if requestedVal, requestedFound := request.Capacity.Requests[name]; requestedFound {
					requestedValPtr = &requestedVal
				}
			}
			consumedCapacity := calculateConsumedCapacity(requestedValPtr, *cap.Consumable)
			consumedCapacities[name] = *consumedCapacity
		}
	}
	return consumedCapacities
}
