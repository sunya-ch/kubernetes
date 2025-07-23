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

package experimental

import (
	"testing"

	. "github.com/onsi/gomega"
	resourceapi "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

const (
	driverA   = "driver-a"
	pool1     = "pool-1"
	device1   = "device-1"
	capacity0 = "capacity-0"
	capacity1 = "capacity-1"
)

var (
	one   = resource.MustParse("1")
	two   = resource.MustParse("2")
	three = resource.MustParse("3")
)

func deviceConsumedCapacity(deviceID DeviceID) DeviceConsumedCapacity {
	capaicty := map[resourceapi.QualifiedName]resource.Quantity{
		capacity0: one,
	}
	return NewDeviceConsumedCapacity(deviceID, capaicty)
}

func TestConsumableCapacity(t *testing.T) {

	t.Run("add-sub-allocating-consumed-capacity", func(t *testing.T) {
		g := NewWithT(t)
		allocatedCapacity := NewConsumedCapacity()
		g.Expect(allocatedCapacity.Empty()).To(BeTrueBecause("allocated capacity should start from zero"))
		oneAllocated := ConsumedCapacity{
			capacity0: &one,
		}
		allocatedCapacity.Add(oneAllocated)
		g.Expect(allocatedCapacity.Empty()).To(BeFalseBecause("capacity is added"))
		allocatedCapacity.Sub(oneAllocated)
		g.Expect(allocatedCapacity.Empty()).To(BeTrueBecause("capacity is subtracted to zero"))
	})

	t.Run("insert-remove-allocating-consumed-capacity-collection", func(t *testing.T) {
		g := NewWithT(t)
		deviceID := MakeDeviceID(driverA, pool1, device1)
		aggregatedCapacity := NewConsumedCapacityCollection()
		aggregatedCapacity.Insert(deviceConsumedCapacity(deviceID))
		aggregatedCapacity.Insert(deviceConsumedCapacity(deviceID))
		allocatedCapacity, found := aggregatedCapacity[deviceID]
		g.Expect(found).To(BeTrueBecause("expected deviceID to be found"))
		g.Expect(allocatedCapacity[capacity0].Cmp(two)).To(BeZero())
		aggregatedCapacity.Remove(deviceConsumedCapacity(deviceID))
		g.Expect(allocatedCapacity[capacity0].Cmp(one)).To(BeZero())
	})

	t.Run("get-consumed-capacity-from-request", func(t *testing.T) {
		requestedCapacity := &resourceapi.CapacityRequirements{
			Requests: map[resourceapi.QualifiedName]resource.Quantity{
				capacity0: one,
				"dummy":   two,
			},
		}
		consumableCapacity := map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
			capacity0: {
				Value: two,
				SharingPolicy: &resourceapi.CapacitySharingPolicy{
					Default:    ptr.To(one),
					ValidRange: &resourceapi.CapacitySharingPolicyRange{Min: one},
				},
			},
			capacity1: {
				Value: two,
				SharingPolicy: &resourceapi.CapacitySharingPolicy{
					Default:    ptr.To(one),
					ValidRange: &resourceapi.CapacitySharingPolicyRange{Min: one},
				},
			},
			// non-consumable
			"dummy": {
				Value: two,
			},
		}
		consumedCapacity := GetConsumedCapacityFromRequest(requestedCapacity, consumableCapacity)
		g := NewWithT(t)
		g.Expect(consumedCapacity).To(HaveLen(2))
		for name, val := range consumedCapacity {
			g.Expect(string(name)).Should(BeElementOf([]string{capacity0, capacity1}))
			g.Expect(val.Cmp(one)).To(BeZero())
		}
	})

	t.Run("violate-capacity-sharing", testViolateCapacitySharingPolicy)

	t.Run("calculate-consumed-capacity", testCalculateConsumedCapacity)

}

func testViolateCapacitySharingPolicy(t *testing.T) {
	testcases := map[string]struct {
		requestedVal resource.Quantity
		consumable   *resourceapi.CapacitySharingPolicy

		expectResult bool
	}{
		"no constraint": {one, nil, false},
		"less than maximum": {
			one,
			&resourceapi.CapacitySharingPolicy{
				Default:    ptr.To(one),
				ValidRange: &resourceapi.CapacitySharingPolicyRange{Min: one, Max: &two},
			},
			false,
		},
		"more than maximum": {
			two,
			&resourceapi.CapacitySharingPolicy{
				Default:    ptr.To(one),
				ValidRange: &resourceapi.CapacitySharingPolicyRange{Min: one, Max: &one},
			},
			true,
		},
		"in set": {
			one,
			&resourceapi.CapacitySharingPolicy{
				Default:     ptr.To(one),
				ValidValues: []resource.Quantity{one},
			},
			false,
		},
		"not in set": {
			two,
			&resourceapi.CapacitySharingPolicy{
				Default:     ptr.To(one),
				ValidValues: []resource.Quantity{one},
			},
			true,
		},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			violate := violatesPolicy(tc.requestedVal, tc.consumable)
			g.Expect(violate).To(BeEquivalentTo(tc.expectResult))
		})
	}
}

func testCalculateConsumedCapacity(t *testing.T) {
	testcases := map[string]struct {
		requestedVal *resource.Quantity
		consumable   resourceapi.CapacitySharingPolicy

		expectResult *resource.Quantity
	}{
		"empty": {nil, resourceapi.CapacitySharingPolicy{}, nil},
		"min in range": {
			nil,
			resourceapi.CapacitySharingPolicy{Default: ptr.To(one), ValidRange: &resourceapi.CapacitySharingPolicyRange{Min: one}},
			&one,
		},
		"default in set": {
			nil,
			resourceapi.CapacitySharingPolicy{Default: ptr.To(one), ValidValues: []resource.Quantity{one}},
			&one,
		},
		"more than min in range": {
			&two,
			resourceapi.CapacitySharingPolicy{Default: ptr.To(one), ValidRange: &resourceapi.CapacitySharingPolicyRange{Min: one}},
			&two,
		},
		"less than min in range": {
			&one,
			resourceapi.CapacitySharingPolicy{Default: ptr.To(one), ValidRange: &resourceapi.CapacitySharingPolicyRange{Min: two}},
			&two,
		},
		"with step (round up)": {
			&two,
			resourceapi.CapacitySharingPolicy{Default: ptr.To(one), ValidRange: &resourceapi.CapacitySharingPolicyRange{Min: one, Step: ptr.To(two.DeepCopy())}},
			&three,
		},
		"with step (no remaining)": {
			&two,
			resourceapi.CapacitySharingPolicy{Default: ptr.To(one), ValidRange: &resourceapi.CapacitySharingPolicyRange{Min: one, Step: ptr.To(one.DeepCopy())}},
			&two,
		},
		"set (round up)": {
			&two,
			resourceapi.CapacitySharingPolicy{Default: ptr.To(one), ValidValues: []resource.Quantity{one, three}},
			&three,
		},
		"larger than set": {
			&three,
			resourceapi.CapacitySharingPolicy{Default: ptr.To(one), ValidValues: []resource.Quantity{one, two}},
			&three,
		},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			consumedCapacity := calculateConsumedCapacity(tc.requestedVal, tc.consumable)
			if tc.expectResult == nil {
				g.Expect(consumedCapacity).To(BeNil())
			} else {
				g.Expect(consumedCapacity.Cmp(*tc.expectResult)).To(BeZero())
			}
		})
	}
}
