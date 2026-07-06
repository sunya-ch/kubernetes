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

package incubating

// Benchmark comparing the pre-commit int64 implementation of roundUpRange
// against the current inf.Dec implementation.
//
// Run benchmarks with raw output:
//   go test -bench=BenchmarkRoundUpRange -benchmem \
//     ./staging/src/k8s.io/dynamic-resource-allocation/structured/internal/incubating/
//
// Run as a formatted table (no external tools needed):
//   go test -v -run=TestRoundUpRangeComparison \
//     ./staging/src/k8s.io/dynamic-resource-allocation/structured/internal/incubating/

import (
	"fmt"
	"os"
	"testing"
	"text/tabwriter"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

// roundUpRangeOld is the pre-commit implementation that used int64 arithmetic.
// Reproduced verbatim from the parent commit so both can be measured in the
// same binary without a checkout.
func roundUpRangeOld(requestedVal *resource.Quantity, validRange *resourceapi.CapacityRequestPolicyRange) resource.Quantity {
	if requestedVal.Cmp(*validRange.Min) < 0 {
		return validRange.Min.DeepCopy()
	}
	if validRange.Step == nil {
		return *requestedVal
	}
	requestedInt64 := requestedVal.Value()
	step := validRange.Step.Value()
	min := validRange.Min.Value()
	added := requestedInt64 - min
	n := added / step
	mod := added % step
	if mod != 0 {
		n += 1
	}
	val := min + step*n
	return *resource.NewQuantity(val, resource.BinarySI)
}

// benchCases covers the interesting paths:
//   - exact hit (no rounding needed)
//   - mid-step (must round up)
//   - fractional step (only the new implementation handles this correctly)
var benchCases = []struct {
	name      string
	requested resource.Quantity
	validRange resourceapi.CapacityRequestPolicyRange
}{
	{
		name:      "integer/exact",
		requested: resource.MustParse("4"),
		validRange: resourceapi.CapacityRequestPolicyRange{
			Min:  ptr.To(resource.MustParse("1")),
			Step: ptr.To(resource.MustParse("1")),
		},
	},
	{
		name:      "integer/mid-step",
		requested: resource.MustParse("3"),
		validRange: resourceapi.CapacityRequestPolicyRange{
			Min:  ptr.To(resource.MustParse("1")),
			Step: ptr.To(resource.MustParse("4")),
		},
	},
	{
		name:      "integer/large",
		requested: resource.MustParse("999"),
		validRange: resourceapi.CapacityRequestPolicyRange{
			Min:  ptr.To(resource.MustParse("100")),
			Step: ptr.To(resource.MustParse("7")),
		},
	},
	{
		// fractional step – old implementation truncates to 0 via int64;
		// included to show correctness difference, not just speed.
		name:      "fractional/step-500m",
		requested: resource.MustParse("1250m"),
		validRange: resourceapi.CapacityRequestPolicyRange{
			Min:  ptr.To(resource.MustParse("500m")),
			Step: ptr.To(resource.MustParse("500m")),
		},
	},
}

func BenchmarkRoundUpRangeOld(b *testing.B) {
	for _, tc := range benchCases {
		b.Run(tc.name, func(b *testing.B) {
			req := tc.requested.DeepCopy()
			vr := tc.validRange
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = roundUpRangeOld(&req, &vr)
			}
		})
	}
}

func BenchmarkRoundUpRangeNew(b *testing.B) {
	for _, tc := range benchCases {
		b.Run(tc.name, func(b *testing.B) {
			req := tc.requested.DeepCopy()
			vr := tc.validRange
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = roundUpRange(&req, &vr)
			}
		})
	}
}

// implSpec pairs a display name with the function under test.
type implSpec struct {
	name string
	fn   func(req *resource.Quantity, vr *resourceapi.CapacityRequestPolicyRange) resource.Quantity
}

// TestRoundUpRangeComparison prints a side-by-side columnar table comparing
// all implementations across all benchmark cases.
//
// Run with: go test -v -run=TestRoundUpRangeComparison
func TestRoundUpRangeComparison(t *testing.T) {
	impls := []implSpec{
		{"old (int64)", roundUpRangeOld},
		{"new (inf.Dec)", roundUpRange},
	}

	type result struct {
		nsOp    float64
		allocsOp uint64
		bytesOp uint64
	}

	// rows[case][impl]
	rows := make([][]result, len(benchCases))
	for ci, tc := range benchCases {
		rows[ci] = make([]result, len(impls))
		for ii, impl := range impls {
			// Capture loop variables for the closure.
			req := tc.requested.DeepCopy()
			vr := tc.validRange
			fn := impl.fn
			br := testing.Benchmark(func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					_ = fn(&req, &vr)
				}
			})
			rows[ci][ii] = result{
				nsOp:    float64(br.NsPerOp()),
				allocsOp: uint64(br.AllocsPerOp()),
				bytesOp: uint64(br.AllocedBytesPerOp()),
			}
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	// Header
	fmt.Fprintf(w, "\ncase\t")
	for _, impl := range impls {
		fmt.Fprintf(w, "%s ns/op\t%s B/op\t%s allocs/op\t", impl.name, impl.name, impl.name)
	}
	fmt.Fprintln(w)

	// Separator
	fmt.Fprintf(w, "----\t")
	for range impls {
		fmt.Fprintf(w, "----------\t----------\t-----------\t")
	}
	fmt.Fprintln(w)

	// Data rows
	for ci, tc := range benchCases {
		fmt.Fprintf(w, "%s\t", tc.name)
		for ii := range impls {
			r := rows[ci][ii]
			fmt.Fprintf(w, "%.1f\t%d\t%d\t", r.nsOp, r.bytesOp, r.allocsOp)
		}
		fmt.Fprintln(w)
	}
	w.Flush()
}
