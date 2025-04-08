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
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	one   = resource.MustParse("1")
	two   = resource.MustParse("2")
	three = resource.MustParse("3")
)

func deviceAllocatedCapacity(deviceID DeviceID) DeviceAllocatedCapacity {
	capaicty := map[v1beta1.QualifiedName]resource.Quantity{
		capacity0: one,
	}
	return NewDeviceAllocatedCapacity(deviceID, capaicty)
}

func TestAllocatedCapacity(t *testing.T) {
	g := NewWithT(t)
	allocatedCapacity := NewAllocatedCapacity()
	g.Expect(allocatedCapacity.Empty()).To(BeTrue())
	oneAllocated := AllocatedCapacity{
		capacity0: &one,
	}
	allocatedCapacity.Add(oneAllocated)
	g.Expect(allocatedCapacity.Empty()).To(BeFalse())
	allocatedCapacity.Sub(oneAllocated)
	g.Expect(allocatedCapacity.Empty()).To(BeTrue())
}

func TestAllocatedCapacityCollection(t *testing.T) {
	g := NewWithT(t)
	deviceID := MakeDeviceID(driverA, pool1, device1)
	aggregatedCapacity := NewAllocatedCapacityCollection()
	aggregatedCapacity.Insert(deviceAllocatedCapacity(deviceID))
	aggregatedCapacity.Insert(deviceAllocatedCapacity(deviceID))
	allocatedCapacity, found := aggregatedCapacity[deviceID]
	g.Expect(found).To(BeTrue())
	g.Expect(allocatedCapacity[capacity0].Cmp(two)).To(BeZero())
	aggregatedCapacity.Remove(deviceAllocatedCapacity(deviceID))
	g.Expect(allocatedCapacity[capacity0].Cmp(one)).To(BeZero())
}

func TestViolateConstraints(t *testing.T) {
	testcases := map[string]struct {
		requestedVal resource.Quantity
		consumable   *v1beta1.CapacityClaimPolicy

		expectResult bool
	}{
		"no constraint": {one, nil, false},
		"less than maximum": {
			one,
			&v1beta1.CapacityClaimPolicy{
				Range: &v1beta1.CapacityClaimPolicyRange{Minimum: one, Maximum: &two},
			},
			false,
		},
		"more than maximum": {
			two,
			&v1beta1.CapacityClaimPolicy{
				Range: &v1beta1.CapacityClaimPolicyRange{Minimum: one, Maximum: &one},
			},
			true,
		},
		"in set": {
			one,
			&v1beta1.CapacityClaimPolicy{
				Set: &v1beta1.CapacityClaimPolicySet{Default: one, Options: []resource.Quantity{one}},
			},
			false,
		},
		"not in set": {
			two,
			&v1beta1.CapacityClaimPolicy{
				Set: &v1beta1.CapacityClaimPolicySet{Default: one, Options: []resource.Quantity{one}},
			},
			true,
		},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			violate := violatePolicy(tc.requestedVal, tc.consumable)
			g.Expect(violate).To(BeEquivalentTo(tc.expectResult))
		})
	}
}

func TestCalculateConsumedCapacity(t *testing.T) {
	testcases := map[string]struct {
		requestedVal *resource.Quantity
		consumable   v1beta1.CapacityClaimPolicy

		expectResult *resource.Quantity
	}{
		"empty": {nil, v1beta1.CapacityClaimPolicy{}, nil},
		"min in range": {
			nil,
			v1beta1.CapacityClaimPolicy{Range: &v1beta1.CapacityClaimPolicyRange{Minimum: one}},
			&one,
		},
		"default in set": {
			nil,
			v1beta1.CapacityClaimPolicy{Set: &v1beta1.CapacityClaimPolicySet{Default: one}},
			&one,
		},
		"more than min in range": {
			&two,
			v1beta1.CapacityClaimPolicy{Range: &v1beta1.CapacityClaimPolicyRange{Minimum: one}},
			&two,
		},
		"less than min in range": {
			&one,
			v1beta1.CapacityClaimPolicy{Range: &v1beta1.CapacityClaimPolicyRange{Minimum: two}},
			&two,
		},
		"with step (round up)": {
			&two,
			v1beta1.CapacityClaimPolicy{Range: &v1beta1.CapacityClaimPolicyRange{Minimum: one, Step: &two}},
			&three,
		},
		"with step (no remaining)": {
			&two,
			v1beta1.CapacityClaimPolicy{Range: &v1beta1.CapacityClaimPolicyRange{Minimum: one, Step: &one}},
			&two,
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

func TestGetConsumedCapacityFromRequest(t *testing.T) {
	requestedCapacity := &v1beta1.CapacityRequirements{
		Requests: map[v1beta1.QualifiedName]resource.Quantity{
			capacity0: one,
			"dummy":   two,
		},
	}
	consumableCapacity := map[v1beta1.QualifiedName]v1beta1.DeviceCapacity{
		capacity0: v1beta1.DeviceCapacity{
			Value: two,
			ClaimPolicy: &v1beta1.CapacityClaimPolicy{
				Range: &v1beta1.CapacityClaimPolicyRange{Minimum: one},
			},
		},
		capacity1: v1beta1.DeviceCapacity{
			Value: two,
			ClaimPolicy: &v1beta1.CapacityClaimPolicy{
				Range: &v1beta1.CapacityClaimPolicyRange{Minimum: one},
			},
		},
		// non-consumable
		"dummy": v1beta1.DeviceCapacity{
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
}
