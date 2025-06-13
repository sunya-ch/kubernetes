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

package validation

import (
	"testing"

	apiresource "k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/kubernetes/pkg/apis/resource"
	"k8s.io/utils/ptr"
)

func testDeviceCapacity(value apiresource.Quantity, policy *resource.CapacitySharingPolicy) resource.DeviceCapacity {
	return resource.DeviceCapacity{
		Value:         value,
		SharingPolicy: policy,
	}
}

func testCapacitySharingPolicy(defaultValue apiresource.Quantity,
	discreteValues *resource.CapacitySharingPolicyDiscrete,
	valueRange *resource.CapacitySharingPolicyRange) *resource.CapacitySharingPolicy {
	return &resource.CapacitySharingPolicy{
		Default:        defaultValue,
		DiscreteValues: discreteValues,
		ValueRange:     valueRange,
	}
}

func testDiscreteValues(options []apiresource.Quantity) *resource.CapacitySharingPolicyDiscrete {
	return &resource.CapacitySharingPolicyDiscrete{
		Options: options,
	}
}

func testValueRange(minimum apiresource.Quantity, maximum, chunksize *apiresource.Quantity) *resource.CapacitySharingPolicyRange {
	return &resource.CapacitySharingPolicyRange{
		Minimum:   minimum,
		Maximum:   maximum,
		ChunkSize: chunksize,
	}
}

func TestValidateDeviceCapacity(t *testing.T) {
	one := apiresource.MustParse("1Gi")
	two := apiresource.MustParse("2Gi")
	maxCapacity := apiresource.MustParse("10Gi")
	overCapacity := apiresource.MustParse("20Gi")

	capacityField := field.NewPath("spec", "devices", "capacity")
	policyField := capacityField.Child("sharingPolicy")
	discreteValuesField := policyField.Child("discreteValues")
	valueRangeField := policyField.Child("valueRange")

	scenarios := map[string]struct {
		capacity     resource.DeviceCapacity
		wantFailures field.ErrorList
	}{
		"no-policy": {
			capacity: testDeviceCapacity(one, nil),
		},
		"valid-simple-range": {
			capacity: testDeviceCapacity(maxCapacity, testCapacitySharingPolicy(one, nil, testValueRange(one, nil, nil))),
		},
		"valid-range-with-maximum": {
			capacity: testDeviceCapacity(maxCapacity, testCapacitySharingPolicy(one, nil, testValueRange(one, ptr.To(maxCapacity), nil))),
		},
		"valid-range-with-chunksize": {
			capacity: testDeviceCapacity(maxCapacity, testCapacitySharingPolicy(one, nil, testValueRange(one, nil, ptr.To(one)))),
		},
		"valid-range-with-maximum-and-chunksize": {
			capacity: testDeviceCapacity(maxCapacity, testCapacitySharingPolicy(one, nil, testValueRange(one, ptr.To(maxCapacity), ptr.To(one)))),
		},
		"valid-single-option": {
			capacity: testDeviceCapacity(maxCapacity, testCapacitySharingPolicy(one, testDiscreteValues([]apiresource.Quantity{one}), nil)),
		},
		"valid-two-options": {
			capacity: testDeviceCapacity(maxCapacity, testCapacitySharingPolicy(one, testDiscreteValues([]apiresource.Quantity{one, maxCapacity}), nil)),
		},
		"default-without-policy": {
			capacity: testDeviceCapacity(maxCapacity, testCapacitySharingPolicy(one, nil, nil)),
			wantFailures: field.ErrorList{
				field.Required(policyField, "exactly one policy of `discreteValues` and `valueRange` is required"),
			},
		},
		"more-than-one-policy": {
			capacity: testDeviceCapacity(maxCapacity, testCapacitySharingPolicy(one,
				testDiscreteValues([]apiresource.Quantity{one}), testValueRange(one, nil, nil))),
			wantFailures: field.ErrorList{
				field.Forbidden(policyField, "exactly one policy can be specified, cannot specify `discreteValues` and `valueRange` at the same time"),
			},
		},
		"invalid-options": {
			capacity: testDeviceCapacity(maxCapacity, testCapacitySharingPolicy(one, testDiscreteValues([]apiresource.Quantity{overCapacity}), nil)),
			wantFailures: field.ErrorList{
				field.Invalid(discreteValuesField.Child("options").Index(0), "20Gi", "option is larger than capacity value: 10Gi"),
				field.NotFound(discreteValuesField.Child("options"), "1Gi"),
			},
		},
		"invalid-options-duplicate": {
			capacity: testDeviceCapacity(maxCapacity, testCapacitySharingPolicy(one, testDiscreteValues([]apiresource.Quantity{one, one}), nil)),
			wantFailures: field.ErrorList{
				field.Duplicate(discreteValuesField.Child("options").Index(1), "1Gi"),
			},
		},
		"invalid-range-large-minimum-small-maximum": {
			capacity: testDeviceCapacity(maxCapacity, testCapacitySharingPolicy(two, nil, testValueRange(overCapacity, ptr.To(one), nil))),
			wantFailures: field.ErrorList{
				field.Invalid(valueRangeField.Child("minimum"), "20Gi", "minimum is larger than capacity value: 10Gi"),
				field.Invalid(valueRangeField.Child("minimum"), "2Gi", "default is less than minimum: 20Gi"),
				field.Invalid(valueRangeField.Child("maximum"), "20Gi", "minimum is larger than maximum: 1Gi"),
				field.Invalid(valueRangeField.Child("maximum"), "2Gi", "default is more than maximum: 1Gi"),
			},
		},
		"invalid-range-large-maximum": {
			capacity: testDeviceCapacity(maxCapacity, testCapacitySharingPolicy(one, nil, testValueRange(one, ptr.To(overCapacity), nil))),
			wantFailures: field.ErrorList{
				field.Invalid(valueRangeField.Child("maximum"), "20Gi", "maximum is larger than capacity value: 10Gi"),
			},
		},
		"invalid-range-mutliple-of-chunksize": {
			capacity: testDeviceCapacity(maxCapacity, testCapacitySharingPolicy(two, nil, testValueRange(one, ptr.To(maxCapacity), ptr.To(two)))),
			wantFailures: field.ErrorList{
				field.Invalid(valueRangeField.Child("chunkSize"), "2Gi", "value is not a multiple of a given chunk size (2Gi) from (1Gi)"),
				field.Invalid(valueRangeField.Child("chunkSize"), "10Gi", "value is not a multiple of a given chunk size (2Gi) from (1Gi)"),
			},
		},
		"invalid-range-large-chunksize": {
			capacity: testDeviceCapacity(maxCapacity, testCapacitySharingPolicy(one, nil, testValueRange(one, nil, ptr.To(maxCapacity)))),
			wantFailures: field.ErrorList{
				field.Invalid(valueRangeField.Child("chunkSize"), "10Gi", "one chunk 11Gi is larger than capacity value: 10Gi"),
			},
		},
	}
	for name, scenario := range scenarios {
		t.Run(name, func(t *testing.T) {
			errs := validateDeviceCapacity(scenario.capacity, capacityField)
			assertFailures(t, scenario.wantFailures, errs)
		})
	}
}
