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
	"errors"

	inf "gopkg.in/inf.v0"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

// CmpRequestOverCapacity checks whether the new capacity request can be added within the given capacity,
// and checks whether the requested value is against the capacity requestPolicy.
func CmpRequestOverCapacity(currentConsumedCapacity ConsumedCapacity, deviceRequestCapacity *resourceapi.CapacityRequirements,
	allowMultipleAllocations *bool, capacity map[resourceapi.QualifiedName]resourceapi.DeviceCapacity, allocatingCapacity ConsumedCapacity) (bool, error) {
	if requestsContainNonExistCapacity(deviceRequestCapacity, capacity) {
		return false, errors.New("some requested capacity has not been defined")
	}
	clone := currentConsumedCapacity.Clone()
	for name, cap := range capacity {
		var requestedValPtr *resource.Quantity
		if deviceRequestCapacity != nil && deviceRequestCapacity.Requests != nil {
			if requestedVal, requestedFound := deviceRequestCapacity.Requests[name]; requestedFound {
				requestedValPtr = &requestedVal
			}
		}
		consumedCapacity := calculateConsumedCapacity(requestedValPtr, cap)
		if violatesPolicy(consumedCapacity, cap.RequestPolicy) {
			return false, nil
		}
		// If the current clone already contains an entry for this capacity, add the consumedCapacity to it.
		// Otherwise, initialize it with calculated consumedCapacity.
		if _, allocatedFound := clone[name]; allocatedFound {
			clone[name].Add(consumedCapacity)
		} else {
			clone[name] = ptr.To(consumedCapacity)
		}
		// If allocatingCapacity contains an entry for this capacity, add its value to clone as well.
		if allocatingVal, allocatingFound := allocatingCapacity[name]; allocatingFound {
			clone[name].Add(*allocatingVal)
		}
		if clone[name].Cmp(cap.Value) > 0 {
			return false, nil
		}
	}
	return true, nil
}

// requestsNonExistCapacity returns true if requests contain non-exist capacity.
func requestsContainNonExistCapacity(deviceRequestCapacity *resourceapi.CapacityRequirements,
	capacity map[resourceapi.QualifiedName]resourceapi.DeviceCapacity) bool {
	if deviceRequestCapacity == nil || deviceRequestCapacity.Requests == nil {
		return false
	}
	for name := range deviceRequestCapacity.Requests {
		if _, found := capacity[name]; !found {
			return true
		}
	}
	return false
}

// calculateConsumedCapacity returns valid capacity to be consumed regarding the requested capacity and device capacity policy.
//
// If no requestPolicy, return capacity.Value.
// If no requestVal, fill the quantity by fillEmptyRequest function
// Otherwise, use requestPolicy to calculate the consumed capacity from request if applicable.
func calculateConsumedCapacity(requestedVal *resource.Quantity, capacity resourceapi.DeviceCapacity) resource.Quantity {
	if requestedVal == nil {
		return fillEmptyRequest(capacity)
	}
	if capacity.RequestPolicy == nil {
		return requestedVal.DeepCopy()
	}
	switch {
	case capacity.RequestPolicy.ValidRange != nil && capacity.RequestPolicy.ValidRange.Min != nil:
		return roundUpRange(requestedVal, capacity.RequestPolicy.ValidRange)
	case capacity.RequestPolicy.ValidValues != nil:
		return roundUpValidValues(requestedVal, capacity.RequestPolicy.ValidValues)
	}
	return *requestedVal
}

// fillEmptyRequest
// return requestPolicy.default if defined.
// Otherwise, return capacity value.
func fillEmptyRequest(capacity resourceapi.DeviceCapacity) resource.Quantity {
	if capacity.RequestPolicy != nil && capacity.RequestPolicy.Default != nil {
		return capacity.RequestPolicy.Default.DeepCopy()
	}
	return capacity.Value.DeepCopy()
}

// roundUpRange rounds the requestedVal up to fit within the specified validRange.
//   - If requestedVal is less than Min, it returns Min.
//   - If Step is specified, it rounds requestedVal up to the nearest multiple of Step
//     starting from Min.
//   - If no Step is specified and requestedVal >= Min, it returns requestedVal as is.
func roundUpRange(requestedVal *resource.Quantity, validRange *resourceapi.CapacityRequestPolicyRange) resource.Quantity {
	if requestedVal.Cmp(*validRange.Min) < 0 {
		return validRange.Min.DeepCopy()
	}
	if validRange.Step == nil {
		return *requestedVal
	}
	// Compute how far requested is above min, then round up to the next
	// multiple of step. All arithmetic uses inf.Dec via Quantity.AsDec() so
	// it is exact at any scale and cannot overflow.
	span := requestedVal.DeepCopy()
	span.Sub(*validRange.Min) // span = requested - min
	stepCopy := validRange.Step.DeepCopy()
	stepDec := stepCopy.AsDec()
	minCopy := validRange.Min.DeepCopy()
	minDec := minCopy.AsDec()

	// n = ceil(span / step)
	var nDec, resultDec, mulDec inf.Dec
	n := nDec.QuoRound(span.AsDec(), stepDec, 0, inf.RoundUp)

	// result = min + n*step, computed entirely in Dec.
	resultDec.Add(minDec, mulDec.Mul(n, stepDec))

	// If the result is a whole number, use NewQuantity to keep the representation compact
	// and compatible with quantities parsed from whole-number strings.
	format := validRange.Step.Format
	if v, ok := resultDec.Unscaled(); ok && resultDec.Scale() == 0 {
		return *resource.NewQuantity(v, format)
	}
	return *resource.NewDecimalQuantity(resultDec, format)
}

// roundUpValidValues returns the first value in validValues that is greater than or equal to requestedVal.
// If no such value exists, it returns requestedVal itself.
func roundUpValidValues(requestedVal *resource.Quantity, validValues []resource.Quantity) resource.Quantity {
	// Simple sequential search is used as the maximum entry of validValues is finite and small (≤10),
	// and the list must already be sorted in ascending order, ensured by API validation.
	// Note: A binary search could alternatively be used for better efficiency if the list grows larger.
	for _, validValue := range validValues {
		if requestedVal.Cmp(validValue) <= 0 {
			return validValue.DeepCopy()
		}
	}
	return *requestedVal
}

// GetConsumedCapacityFromRequest returns valid consumed capacity,
// according to claim request and defined capacity.
func GetConsumedCapacityFromRequest(requestedCapacity *resourceapi.CapacityRequirements,
	consumableCapacity map[resourceapi.QualifiedName]resourceapi.DeviceCapacity) map[resourceapi.QualifiedName]resource.Quantity {
	consumedCapacity := make(map[resourceapi.QualifiedName]resource.Quantity)
	for name, cap := range consumableCapacity {
		var requestedValPtr *resource.Quantity
		if requestedCapacity != nil && requestedCapacity.Requests != nil {
			if requestedVal, requestedFound := requestedCapacity.Requests[name]; requestedFound {
				requestedValPtr = &requestedVal
			}
		}
		capacity := calculateConsumedCapacity(requestedValPtr, cap)
		consumedCapacity[name] = capacity
	}
	return consumedCapacity
}

// violatesPolicy checks whether the request violate the requestPolicy.
func violatesPolicy(requestedVal resource.Quantity, policy *resourceapi.CapacityRequestPolicy) bool {
	if policy == nil {
		// no policy to check
		return false
	}
	if policy.Default != nil && requestedVal == *policy.Default {
		return false
	}
	switch {
	case policy.ValidRange != nil:
		return violateValidRange(requestedVal, *policy.ValidRange)
	case len(policy.ValidValues) > 0:
		return violateValidValues(requestedVal, policy.ValidValues)
	}
	// no policy violated through to completion.
	return false
}

func violateValidRange(requestedVal resource.Quantity, validRange resourceapi.CapacityRequestPolicyRange) bool {
	if validRange.Max != nil &&
		requestedVal.Cmp(*validRange.Max) > 0 {
		return true
	}
	if validRange.Step != nil {
		// span = requested - min; check span is a whole-number multiple of step.
		// DeepCopy before AsDec to avoid mutating the originals.
		span := requestedVal.DeepCopy()
		span.Sub(*validRange.Min)
		spanDec := span.AsDec()
		stepCopy := validRange.Step.DeepCopy()
		stepDec := stepCopy.AsDec()
		// n = floor(span / step); if n*step != span then it's not aligned.
		var nDec, remDec, mulDec inf.Dec
		n := nDec.QuoRound(spanDec, stepDec, 0, inf.RoundDown)
		remainder := remDec.Sub(spanDec, mulDec.Mul(n, stepDec))
		if remainder.Sign() != 0 {
			return true
		}
	}
	return false
}

func violateValidValues(requestedVal resource.Quantity, validValues []resource.Quantity) bool {
	for _, validVal := range validValues {
		if requestedVal.Cmp(validVal) == 0 {
			return false
		}
	}
	return true
}
