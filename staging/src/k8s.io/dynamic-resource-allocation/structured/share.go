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
	"k8s.io/apimachinery/pkg/util/sets"
	draapi "k8s.io/dynamic-resource-allocation/api"
	"k8s.io/klog/v2"
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

// distinctAttributeConstraint compares an attribute value across devices.
// All devices must share the same value. When the set of devices is
// empty, any device that has the attribute can be added. After that,
// only matching devices can be added.
//
// We don't need to track *which* devices are part of the set, only
// how many.
type distinctAttributeConstraint struct {
	logger        klog.Logger // Includes name and attribute name, so no need to repeat in log messages.
	requestNames  sets.Set[string]
	attributeName draapi.FullyQualifiedName

	attributes map[string]draapi.DeviceAttribute
	numDevices int
}

func (m *distinctAttributeConstraint) add(requestName string, device *draapi.BasicDevice, deviceID DeviceID) bool {
	if m.requestNames.Len() > 0 && !m.requestNames.Has(requestName) {
		// Device not affected by constraint.
		m.logger.V(7).Info("Constraint does not apply to request", "request", requestName)
		return true
	}

	attribute := lookupAttribute(device, deviceID, m.attributeName)
	if attribute == nil {
		// Doesn't have the attribute.
		m.logger.V(7).Info("Constraint not satisfied, attribute not set")
		return false
	}

	if m.numDevices == 0 {
		// The first device can always get picked.
		m.attributes[requestName] = *attribute
		m.numDevices = 1
		m.logger.V(7).Info("First attribute added")
		return true
	}

	if !m.distinctAttribute(*attribute) {
		m.logger.V(7).Info("Constraint not satisfied, duplicated attribute")
	}
	m.attributes[requestName] = *attribute
	m.numDevices++
	m.logger.V(7).Info("Constraint satisfied by device", "device", deviceID, "numDevices", m.numDevices)
	return true
}

func (m *distinctAttributeConstraint) remove(requestName string, device *draapi.BasicDevice, deviceID DeviceID) {
	if m.requestNames.Len() > 0 && !m.requestNames.Has(requestName) {
		// Device not affected by constraint.
		return
	}
	delete(m.attributes, requestName)
	m.numDevices--
	m.logger.V(7).Info("Device removed from constraint set", "device", deviceID, "numDevices", m.numDevices)
}

func (m *distinctAttributeConstraint) distinctAttribute(attribute draapi.DeviceAttribute) bool {
	for _, attr := range m.attributes {
		switch {
		case attribute.StringValue != nil:
			if attr.StringValue != nil && attribute.StringValue == attr.StringValue {
				m.logger.V(7).Info("String values duplicated")
				return false
			}
		case attribute.IntValue != nil:
			if attr.IntValue != nil && attribute.IntValue == attr.IntValue {
				m.logger.V(7).Info("Int values duplicated")
				return false
			}
		case attribute.BoolValue != nil:
			if attr.BoolValue != nil && attribute.BoolValue == attr.BoolValue {
				m.logger.V(7).Info("Bool values duplicated")
				return false
			}
		case attribute.VersionValue != nil:
			// semver 2.0.0 requires that version strings are in their
			// minimal form (in particular, no leading zeros). Therefore a
			// strict "exact equal" check can do a string comparison.
			if attr.VersionValue != nil && attribute.VersionValue == attr.VersionValue {
				m.logger.V(7).Info("Version values duplicated")
				return false
			}
		default:
			// Unknown value type, cannot match.
			m.logger.V(7).Info("Match attribute type unknown")
			return false
		}
	}
	return true
}
