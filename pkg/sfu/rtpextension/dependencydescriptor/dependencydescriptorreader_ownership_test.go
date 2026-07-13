// Copyright 2026 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dependencydescriptor

import (
	"fmt"
	"reflect"
	"runtime"
	"testing"
)

func TestDependencyDescriptorReaderSteadyStateDoesNotMutateTemplates(t *testing.T) {
	packets := dependencyDescriptorSteadyStatePackets(t)
	for i, packet := range packets {
		t.Run(fmt.Sprintf("packet_%d", i), func(t *testing.T) {
			structure := dependencyDescriptorTestStructure(t)
			before := dependencyDescriptorTemplateSnapshot(structure.Templates)

			first := dependencyDescriptorTestParse(t, packet, structure)
			want := dependencyDescriptorTemplateSnapshot([]*FrameDependencyTemplate{first.FrameDependencies})[0]
			if len(first.FrameDependencies.DecodeTargetIndications) == 0 ||
				len(first.FrameDependencies.FrameDiffs) == 0 ||
				len(first.FrameDependencies.ChainDiffs) == 0 {
				t.Fatalf("steady-state packet did not expose every mutable dependency slice: %#v", first.FrameDependencies)
			}

			first.FrameDependencies.DecodeTargetIndications[0] = (first.FrameDependencies.DecodeTargetIndications[0] + 1) % 4
			first.FrameDependencies.FrameDiffs[0]++
			first.FrameDependencies.ChainDiffs[0]++

			if !reflect.DeepEqual(structure.Templates, before) {
				t.Fatal("structure templates changed after mutating steady-state descriptor")
			}

			second := dependencyDescriptorTestParse(t, packet, structure)
			if !reflect.DeepEqual(second.FrameDependencies, want) {
				t.Fatalf("second steady-state descriptor = %#v, want %#v", second.FrameDependencies, want)
			}
		})
	}
}

func TestFrameDependencyTemplateCloneOwnsSliceBacking(t *testing.T) {
	original := &FrameDependencyTemplate{
		SpatialId:               1,
		TemporalId:              2,
		DecodeTargetIndications: []DecodeTargetIndication{DecodeTargetRequired, DecodeTargetDiscardable},
		FrameDiffs:              []int{3},
		ChainDiffs:              []int{4},
	}
	want := dependencyDescriptorTemplateSnapshot([]*FrameDependencyTemplate{original})[0]

	clone := original.Clone()
	runtime.GC()
	clone.SpatialId = 3
	clone.TemporalId = 4
	clone.DecodeTargetIndications[0] = DecodeTargetNotPresent
	clone.DecodeTargetIndications = append(clone.DecodeTargetIndications, DecodeTargetSwitch)
	if clone.FrameDiffs[0] != 3 {
		t.Fatalf("appending decode target indications changed frame diffs: %#v", clone)
	}
	clone.FrameDiffs[0] = 5
	clone.FrameDiffs = append(clone.FrameDiffs, 6)
	if clone.FrameDiffs[0] != 5 || clone.ChainDiffs[0] != 4 {
		t.Fatalf("appending one cloned slice changed another: %#v", clone)
	}
	clone.ChainDiffs[0] = 7

	if !reflect.DeepEqual(original, want) {
		t.Fatalf("original template changed after mutating its clone:\n got: %#v\nwant: %#v", original, want)
	}
}
