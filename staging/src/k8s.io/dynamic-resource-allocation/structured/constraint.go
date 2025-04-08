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
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	draapi "k8s.io/dynamic-resource-allocation/api"
	"k8s.io/klog/v2"
)

type constraint interface {
	// add is called whenever a device is about to be allocated. It must
	// check whether the device matches the constraint and if yes,
	// track that it is allocated.
	add(requestName string, device *draapi.BasicDevice, deviceID DeviceID) bool

	// For every successful add there is exactly one matching removed call
	// with the exact same parameters.
	remove(requestName string, device *draapi.BasicDevice, deviceID DeviceID)
}

// matchAttributeConstraint compares an attribute value across devices.
// All devices must share the same value. When the set of devices is
// empty, any device that has the attribute can be added. After that,
// only matching devices can be added.
//
// We don't need to track *which* devices are part of the set, only
// how many.
type matchAttributeConstraint struct {
	logger        klog.Logger // Includes name and attribute name, so no need to repeat in log messages.
	requestNames  sets.Set[string]
	attributeName draapi.FullyQualifiedName

	attribute  *draapi.DeviceAttribute
	numDevices int
}

func (m *matchAttributeConstraint) add(requestName string, device *draapi.BasicDevice, deviceID DeviceID) bool {
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
		m.attribute = attribute
		m.numDevices = 1
		m.logger.V(7).Info("First in set")
		return true
	}

	switch {
	case attribute.StringValue != nil:
		if m.attribute.StringValue == nil || *attribute.StringValue != *m.attribute.StringValue {
			m.logger.V(7).Info("String values different")
			return false
		}
	case attribute.IntValue != nil:
		if m.attribute.IntValue == nil || *attribute.IntValue != *m.attribute.IntValue {
			m.logger.V(7).Info("Int values different")
			return false
		}
	case attribute.BoolValue != nil:
		if m.attribute.BoolValue == nil || *attribute.BoolValue != *m.attribute.BoolValue {
			m.logger.V(7).Info("Bool values different")
			return false
		}
	case attribute.VersionValue != nil:
		// semver 2.0.0 requires that version strings are in their
		// minimal form (in particular, no leading zeros). Therefore a
		// strict "exact equal" check can do a string comparison.
		if m.attribute.VersionValue == nil || *attribute.VersionValue != *m.attribute.VersionValue {
			m.logger.V(7).Info("Version values different")
			return false
		}
	default:
		// Unknown value type, cannot match.
		m.logger.V(7).Info("Match attribute type unknown")
		return false
	}

	m.numDevices++
	m.logger.V(7).Info("Constraint satisfied by device", "device", deviceID, "numDevices", m.numDevices)
	return true
}

func (m *matchAttributeConstraint) remove(requestName string, device *draapi.BasicDevice, deviceID DeviceID) {
	if m.requestNames.Len() > 0 && !m.requestNames.Has(requestName) {
		// Device not affected by constraint.
		return
	}

	m.numDevices--
	m.logger.V(7).Info("Device removed from constraint set", "device", deviceID, "numDevices", m.numDevices)
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

func lookupAttribute(device *draapi.BasicDevice, deviceID DeviceID, attributeName draapi.FullyQualifiedName) *draapi.DeviceAttribute {
	// Fully-qualified match?
	if attr, ok := device.Attributes[draapi.QualifiedName(attributeName)]; ok {
		return &attr
	}
	index := strings.Index(string(attributeName), "/")
	if index < 0 {
		// Should not happen for a valid fully qualified name.
		return nil
	}

	if string(attributeName[0:index]) != deviceID.Driver.String() {
		// Not an attribute of the driver and not found above,
		// so it is not available.
		return nil
	}

	// Domain matches the driver, so let's check just the ID.
	if attr, ok := device.Attributes[draapi.QualifiedName(attributeName[index+1:])]; ok {
		return &attr
	}

	return nil
}
