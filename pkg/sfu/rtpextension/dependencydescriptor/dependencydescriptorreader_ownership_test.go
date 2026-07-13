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
	"slices"
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

func TestFrameDependencyTemplateClonePreservesEmptyAndNonEmptySliceOwnership(t *testing.T) {
	for _, dtiLen := range []int{0, 1} {
		for _, frameDiffsLen := range []int{0, 1} {
			for _, chainDiffsLen := range []int{0, 1} {
				t.Run(fmt.Sprintf("dti_%d_frame_diffs_%d_chain_diffs_%d", dtiLen, frameDiffsLen, chainDiffsLen), func(t *testing.T) {
					assertFrameDependencyTemplateCloneOwnsSlices(t, frameDependencyTemplateWithSliceLengths(dtiLen, frameDiffsLen, chainDiffsLen))
				})
			}
		}
	}
}

func TestFrameDependencyTemplateCloneFallsBackForLargerSlices(t *testing.T) {
	for _, test := range []struct {
		name                                string
		decodeTargetIndications, frameDiffs int
		chainDiffs                          int
	}{
		{name: "decode_target_indications", decodeTargetIndications: inlineDecodeTargetIndications + 1, frameDiffs: 1, chainDiffs: 1},
		{name: "frame_diffs", decodeTargetIndications: 1, frameDiffs: inlineFrameDiffs + 1, chainDiffs: 1},
		{name: "chain_diffs", decodeTargetIndications: 1, frameDiffs: 1, chainDiffs: inlineChainDiffs + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertFrameDependencyTemplateCloneOwnsSlices(t, frameDependencyTemplateWithSliceLengths(test.decodeTargetIndications, test.frameDiffs, test.chainDiffs))
		})
	}
}

func TestFrameDependencyTemplateCloneRetainsAppendedSlicesAcrossGC(t *testing.T) {
	clone := frameDependencyTemplateWithSliceLengths(inlineDecodeTargetIndications, inlineFrameDiffs, inlineChainDiffs).Clone()
	wantDecodeTargetIndications := append(slices.Clone(clone.DecodeTargetIndications), DecodeTargetSwitch)
	wantFrameDiffs := append(slices.Clone(clone.FrameDiffs), 17)
	wantChainDiffs := append(slices.Clone(clone.ChainDiffs), 19)
	clone.DecodeTargetIndications = append(clone.DecodeTargetIndications, DecodeTargetSwitch)
	clone.FrameDiffs = append(clone.FrameDiffs, 17)
	clone.ChainDiffs = append(clone.ChainDiffs, 19)

	pressure := dependencyDescriptorCloneGCPressure()

	if got := clone.DecodeTargetIndications; !slices.Equal(got, wantDecodeTargetIndications) {
		t.Fatalf("DecodeTargetIndications after GC = %v, want %v", got, wantDecodeTargetIndications)
	}
	if got := clone.FrameDiffs; !slices.Equal(got, wantFrameDiffs) {
		t.Fatalf("FrameDiffs after GC = %v, want %v", got, wantFrameDiffs)
	}
	if got := clone.ChainDiffs; !slices.Equal(got, wantChainDiffs) {
		t.Fatalf("ChainDiffs after GC = %v, want %v", got, wantChainDiffs)
	}

	runtime.KeepAlive(clone)
	runtime.KeepAlive(pressure)
}

func TestDependencyDescriptorReaderCustomFrameDiffsRetainResultAcrossGC(t *testing.T) {
	structure := dependencyDescriptorTestStructure(t)
	packet, want, template := dependencyDescriptorCustomOverridePacketWithTemplate(t, structure)
	if len(want.FrameDependencies.FrameDiffs) <= len(template.FrameDiffs) {
		t.Fatalf("custom frame diffs = %d, selected template frame diffs = %d; test must grow the cloned slice", len(want.FrameDependencies.FrameDiffs), len(template.FrameDiffs))
	}

	decoded := dependencyDescriptorTestParse(t, packet, structure)
	pressure := dependencyDescriptorCloneGCPressure()

	if got := decoded.FrameDependencies.FrameDiffs; !slices.Equal(got, want.FrameDependencies.FrameDiffs) {
		t.Fatalf("FrameDiffs after GC = %v, want %v", got, want.FrameDependencies.FrameDiffs)
	}

	runtime.KeepAlive(decoded)
	runtime.KeepAlive(pressure)
}

func assertFrameDependencyTemplateCloneOwnsSlices(t *testing.T, original *FrameDependencyTemplate) {
	t.Helper()

	before := dependencyDescriptorTemplateSnapshot([]*FrameDependencyTemplate{original})[0]
	clone := original.Clone()
	if clone.DecodeTargetIndications == nil || clone.FrameDiffs == nil || clone.ChainDiffs == nil {
		t.Fatalf("Clone returned a nil empty slice: %#v", clone)
	}
	if clone.SpatialId != original.SpatialId || clone.TemporalId != original.TemporalId ||
		!slices.Equal(clone.DecodeTargetIndications, original.DecodeTargetIndications) ||
		!slices.Equal(clone.FrameDiffs, original.FrameDiffs) ||
		!slices.Equal(clone.ChainDiffs, original.ChainDiffs) {
		t.Fatalf("Clone = %#v, want value copy of %#v", clone, original)
	}

	if len(clone.DecodeTargetIndications) != 0 {
		clone.DecodeTargetIndications[0] = DecodeTargetNotPresent
	}
	if len(clone.FrameDiffs) != 0 {
		clone.FrameDiffs[0] = -1
	}
	if len(clone.ChainDiffs) != 0 {
		clone.ChainDiffs[0] = -1
	}
	clone.DecodeTargetIndications = append(clone.DecodeTargetIndications, DecodeTargetSwitch)
	clone.FrameDiffs = append(clone.FrameDiffs, 17)
	clone.ChainDiffs = append(clone.ChainDiffs, 19)

	if !reflect.DeepEqual(original, before) {
		t.Fatalf("original template changed after mutating its clone:\n got: %#v\nwant: %#v", original, before)
	}
	if got := clone.DecodeTargetIndications[len(clone.DecodeTargetIndications)-1]; got != DecodeTargetSwitch {
		t.Fatalf("appended decode target indication = %d, want %d", got, DecodeTargetSwitch)
	}
	if got := clone.FrameDiffs[len(clone.FrameDiffs)-1]; got != 17 {
		t.Fatalf("appended frame diff = %d, want 17", got)
	}
	if got := clone.ChainDiffs[len(clone.ChainDiffs)-1]; got != 19 {
		t.Fatalf("appended chain diff = %d, want 19", got)
	}
}

func frameDependencyTemplateWithSliceLengths(dtiLen, frameDiffsLen, chainDiffsLen int) *FrameDependencyTemplate {
	template := &FrameDependencyTemplate{
		SpatialId:  1,
		TemporalId: 2,
	}
	if dtiLen != 0 {
		template.DecodeTargetIndications = make([]DecodeTargetIndication, dtiLen)
		for i := range template.DecodeTargetIndications {
			template.DecodeTargetIndications[i] = DecodeTargetRequired
		}
	}
	if frameDiffsLen != 0 {
		template.FrameDiffs = make([]int, frameDiffsLen)
		for i := range template.FrameDiffs {
			template.FrameDiffs[i] = i + 1
		}
	}
	if chainDiffsLen != 0 {
		template.ChainDiffs = make([]int, chainDiffsLen)
		for i := range template.ChainDiffs {
			template.ChainDiffs[i] = i + 1
		}
	}
	return template
}

func dependencyDescriptorCloneGCPressure() [][]int {
	runtime.GC()

	pressure := make([][]int, 4096)
	for i := range pressure {
		pressure[i] = []int{i, ^i}
	}
	runtime.GC()

	return pressure
}
