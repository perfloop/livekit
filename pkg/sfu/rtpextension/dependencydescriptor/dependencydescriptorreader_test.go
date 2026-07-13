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
	"encoding/hex"
	"reflect"
	"slices"
	"testing"
)

// dependencyDescriptorStructureHex is a dependency-descriptor packet captured
// from traffic. It carries the structure used by the steady-state packets below.
const dependencyDescriptorStructureHex = "c1017280081485214eafffaaaa863cf0430c10c302afc0aaa0063c00430010c002a000a80006000040001d954926e082b04a0941b820ac1282503157f974000ca864330e222222eca8655304224230eca877530077004200ef008601df010d"

var dependencyDescriptorSteadyStateHex = []string{
	"860173",
	"460173",
	"8b0174",
	"0b0174",
	"c30175",
}

func TestDependencyDescriptorReaderCustomOverrides(t *testing.T) {
	structure := dependencyDescriptorTestStructure(t)
	packet, want := dependencyDescriptorCustomOverridePacket(t, structure)

	got := dependencyDescriptorTestParse(t, packet, structure)
	if got.FrameNumber != want.FrameNumber {
		t.Fatalf("FrameNumber = %d, want %d", got.FrameNumber, want.FrameNumber)
	}
	if got.FrameDependencies.SpatialId != want.FrameDependencies.SpatialId ||
		got.FrameDependencies.TemporalId != want.FrameDependencies.TemporalId {
		t.Fatalf("layer = (%d, %d), want (%d, %d)",
			got.FrameDependencies.SpatialId,
			got.FrameDependencies.TemporalId,
			want.FrameDependencies.SpatialId,
			want.FrameDependencies.TemporalId,
		)
	}
	if !slices.Equal(got.FrameDependencies.DecodeTargetIndications, want.FrameDependencies.DecodeTargetIndications) {
		t.Errorf("DecodeTargetIndications = %v, want %v", got.FrameDependencies.DecodeTargetIndications, want.FrameDependencies.DecodeTargetIndications)
	}
	if !slices.Equal(got.FrameDependencies.FrameDiffs, want.FrameDependencies.FrameDiffs) {
		t.Errorf("FrameDiffs = %v, want %v", got.FrameDependencies.FrameDiffs, want.FrameDependencies.FrameDiffs)
	}
	if !slices.Equal(got.FrameDependencies.ChainDiffs, want.FrameDependencies.ChainDiffs) {
		t.Errorf("ChainDiffs = %v, want %v", got.FrameDependencies.ChainDiffs, want.FrameDependencies.ChainDiffs)
	}
}

func TestDependencyDescriptorReaderCustomOverridesDoNotMutateTemplates(t *testing.T) {
	structure := dependencyDescriptorTestStructure(t)
	before := dependencyDescriptorTemplateSnapshot(structure.Templates)
	packet, _ := dependencyDescriptorCustomOverridePacket(t, structure)

	decoded := dependencyDescriptorTestParse(t, packet, structure)
	decoded.FrameDependencies.DecodeTargetIndications[0] = DecodeTargetNotPresent
	decoded.FrameDependencies.FrameDiffs[0] = -1
	decoded.FrameDependencies.ChainDiffs[0] = -1

	packets := dependencyDescriptorSteadyStatePackets(t)
	_ = dependencyDescriptorTestParse(t, packets[0], structure)

	if !reflect.DeepEqual(structure.Templates, before) {
		t.Fatalf("structure templates changed after mutating decoded custom overrides:\n got: %#v\nwant: %#v", structure.Templates, before)
	}
}

func dependencyDescriptorTestStructure(tb testing.TB) *FrameDependencyStructure {
	tb.Helper()

	packet, err := hex.DecodeString(dependencyDescriptorStructureHex)
	if err != nil {
		tb.Fatalf("decode structure packet: %v", err)
	}

	decoded := dependencyDescriptorTestParse(tb, packet, nil)
	if decoded.AttachedStructure == nil {
		tb.Fatal("structure packet did not attach a frame dependency structure")
	}
	return decoded.AttachedStructure
}

func dependencyDescriptorSteadyStatePackets(tb testing.TB) [][]byte {
	tb.Helper()

	packets := make([][]byte, len(dependencyDescriptorSteadyStateHex))
	for i, encoded := range dependencyDescriptorSteadyStateHex {
		packet, err := hex.DecodeString(encoded)
		if err != nil {
			tb.Fatalf("decode steady-state packet %d: %v", i, err)
		}
		packets[i] = packet
	}
	return packets
}

func dependencyDescriptorCustomOverridePacket(tb testing.TB, structure *FrameDependencyStructure) ([]byte, *DependencyDescriptor) {
	tb.Helper()

	packet, want, _ := dependencyDescriptorCustomOverridePacketWithTemplate(tb, structure)
	return packet, want
}

func dependencyDescriptorCustomOverridePacketWithTemplate(tb testing.TB, structure *FrameDependencyStructure) ([]byte, *DependencyDescriptor, *FrameDependencyTemplate) {
	tb.Helper()

	want := &DependencyDescriptor{
		FirstPacketInFrame: true,
		LastPacketInFrame:  true,
		FrameNumber:        0x1234,
		FrameDependencies: &FrameDependencyTemplate{
			SpatialId:  0,
			TemporalId: 0,
			DecodeTargetIndications: []DecodeTargetIndication{
				DecodeTargetRequired,
				DecodeTargetDiscardable,
				DecodeTargetNotPresent,
				DecodeTargetSwitch,
				DecodeTargetRequired,
				DecodeTargetDiscardable,
				DecodeTargetNotPresent,
				DecodeTargetSwitch,
				DecodeTargetRequired,
			},
			FrameDiffs: []int{2, 17, 257},
			ChainDiffs: []int{42, 69, 101},
		},
	}

	writer, err := NewDependencyDescriptorWriter(nil, structure, ^uint32(0), want)
	if err != nil {
		tb.Fatalf("create custom-override writer: %v", err)
	}
	if !writer.bestTemplate.NeedCustomDtis || !writer.bestTemplate.NeedCustomFdiffs || !writer.bestTemplate.NeedCustomChains {
		tb.Fatalf("custom override flags = %+v, want DTI, frame-diff, and chain overrides", writer.bestTemplate)
	}

	packet := make([]byte, (writer.ValueSizeBits()+7)/8)
	writer.ResetBuf(packet)
	if err := writer.Write(); err != nil {
		tb.Fatalf("write custom-override packet: %v", err)
	}
	return packet, want, structure.Templates[writer.bestTemplate.TemplateIdx]
}

func dependencyDescriptorTestParse(tb testing.TB, packet []byte, structure *FrameDependencyStructure) *DependencyDescriptor {
	tb.Helper()

	decoded := &DependencyDescriptor{}
	extension := DependencyDescriptorExtension{
		Descriptor: decoded,
		Structure:  structure,
	}
	if _, err := extension.Unmarshal(packet); err != nil {
		tb.Fatalf("parse dependency descriptor: %v", err)
	}
	return decoded
}

func dependencyDescriptorTemplateSnapshot(templates []*FrameDependencyTemplate) []*FrameDependencyTemplate {
	snapshot := make([]*FrameDependencyTemplate, len(templates))
	for i, template := range templates {
		snapshot[i] = &FrameDependencyTemplate{
			SpatialId:               template.SpatialId,
			TemporalId:              template.TemporalId,
			DecodeTargetIndications: slices.Clone(template.DecodeTargetIndications),
			FrameDiffs:              slices.Clone(template.FrameDiffs),
			ChainDiffs:              slices.Clone(template.ChainDiffs),
		}
	}
	return snapshot
}
