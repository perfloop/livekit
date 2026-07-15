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

package videolayerselector

import (
	"bytes"
	"slices"
	"testing"

	"github.com/pion/rtp"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	dd "github.com/livekit/livekit-server/pkg/sfu/rtpextension/dependencydescriptor"
	"github.com/livekit/protocol/logger"
)

func TestDependencyDescriptorSelectMarshalBehavior(t *testing.T) {
	const initialFrame = uint64(100)

	activeDecodeTargets := uint32(1)
	selector := newDependencyDescriptorPerfSelector()
	initialStructure := newDependencyDescriptorPerfStructure(0)

	first := requireDependencyDescriptorSelectedExtension(
		t,
		selector,
		newDependencyDescriptorPerfPacket(
			initialFrame,
			initialFrame,
			0,
			initialStructure,
			true,
			&activeDecodeTargets,
		),
		initialStructure,
		&activeDecodeTargets,
	)
	firstSnapshot := bytes.Clone(first)

	second := requireDependencyDescriptorSelectedExtension(
		t,
		selector,
		newDependencyDescriptorPerfPacket(
			initialFrame+1,
			initialFrame,
			1,
			nil,
			false,
			nil,
		),
		initialStructure,
		&activeDecodeTargets,
	)
	third := requireDependencyDescriptorSelectedExtension(
		t,
		selector,
		newDependencyDescriptorPerfPacket(
			initialFrame+2,
			initialFrame,
			1,
			nil,
			false,
			nil,
		),
		initialStructure,
		&activeDecodeTargets,
	)

	if bytes.Equal(second, third) {
		t.Fatal("successive descriptors produced identical extensions")
	}
	requireDependencyDescriptorDistinctBacking(t, first, second)
	requireDependencyDescriptorDistinctBacking(t, second, third)
	requireDependencyDescriptorDistinctBacking(t, first, third)

	// The selector retains the input structure after it is attached. A later
	// malformed packet can therefore expose a mismatch between that structure's
	// NumChains and the packet's ChainDiffs while the selector still has the
	// original one-chain frame state. Select must recover from the marshal panic
	// and accept a subsequent valid structure update.
	initialStructure.NumChains = 2
	malformed := selector.Select(
		newDependencyDescriptorPerfPacket(
			initialFrame+3,
			initialFrame,
			1,
			nil,
			false,
			nil,
		),
		0,
	)
	if malformed.IsSelected {
		t.Fatal("malformed descriptor was selected")
	}

	refreshedStructure := newDependencyDescriptorPerfStructure(1)
	refreshed := requireDependencyDescriptorSelectedExtension(
		t,
		selector,
		newDependencyDescriptorPerfPacket(
			initialFrame+4,
			initialFrame+4,
			0,
			refreshedStructure,
			true,
			&activeDecodeTargets,
		),
		refreshedStructure,
		&activeDecodeTargets,
	)

	if !bytes.Equal(first, firstSnapshot) {
		t.Fatal("later selections changed the first returned extension")
	}
	requireDependencyDescriptorDistinctBacking(t, first, refreshed)
	requireDependencyDescriptorDistinctBacking(t, third, refreshed)
}

func TestDependencyDescriptorSelectMarshalMultiLayerBehavior(t *testing.T) {
	selector := NewDependencyDescriptor(logger.GetLogger())
	selector.SetTarget(buffer.VideoLayer{Spatial: 2, Temporal: 2})
	selector.SetRequestSpatial(2)

	firstStructureFrames := createDDFrames(buffer.VideoLayer{Spatial: 2, Temporal: 2}, 100)
	firstStructureFrames[1].DependencyDescriptor.Descriptor.FrameDependencies.FrameDiffs = []int{1}
	first := requireDependencyDescriptorSelectedExtensions(t, selector, firstStructureFrames)
	firstSnapshot := bytes.Clone(first)

	refreshedStructureFrames := createDDFrames(buffer.VideoLayer{Spatial: 2, Temporal: 1}, 1000)
	refreshedStructureFrames[0].DependencyDescriptor.Descriptor.AttachedStructure.StructureId = 1
	refreshedStructureFrames[1].DependencyDescriptor.Descriptor.FrameDependencies.FrameDiffs = []int{1}
	last := requireDependencyDescriptorSelectedExtensions(t, selector, refreshedStructureFrames)

	if !bytes.Equal(first, firstSnapshot) {
		t.Fatal("structure refresh changed a previously returned extension")
	}
	requireDependencyDescriptorDistinctBacking(t, first, last)
}

func BenchmarkDependencyDescriptorSelectMarshal(b *testing.B) {
	const initialFrame = uint64(1)

	activeDecodeTargets := uint32(1)
	selector := newDependencyDescriptorPerfSelector()
	structure := newDependencyDescriptorPerfStructure(0)
	warmup := selector.Select(
		newDependencyDescriptorPerfPacket(
			initialFrame,
			initialFrame,
			0,
			structure,
			true,
			&activeDecodeTargets,
		),
		0,
	)
	if !warmup.IsSelected || len(warmup.DependencyDescriptorExtension) == 0 {
		b.Fatal("warmup descriptor was not selected")
	}

	packet := newDependencyDescriptorPerfPacket(
		initialFrame+1,
		initialFrame,
		1,
		nil,
		false,
		nil,
	)
	frame := initialFrame + 1
	for b.Loop() {
		packet.DependencyDescriptor.Descriptor.FrameNumber = uint16(frame)
		packet.DependencyDescriptor.ExtFrameNum = frame
		packet.Packet.SequenceNumber = uint16(frame)

		result := selector.Select(packet, 0)
		if !result.IsSelected || len(result.DependencyDescriptorExtension) == 0 {
			b.Fatalf("frame %d was not selected", frame)
		}
		frame++
	}
}

func newDependencyDescriptorPerfSelector() *DependencyDescriptor {
	selector := NewDependencyDescriptor(logger.GetLogger())
	selector.SetTarget(buffer.VideoLayer{Spatial: 0, Temporal: 0})
	selector.SetRequestSpatial(0)
	return selector
}

func newDependencyDescriptorPerfStructure(structureID int) *dd.FrameDependencyStructure {
	return &dd.FrameDependencyStructure{
		StructureId:                  structureID,
		NumDecodeTargets:             1,
		NumChains:                    1,
		DecodeTargetProtectedByChain: []int{0},
		Templates: []*dd.FrameDependencyTemplate{
			{
				SpatialId:               0,
				TemporalId:              0,
				DecodeTargetIndications: []dd.DecodeTargetIndication{dd.DecodeTargetRequired},
				ChainDiffs:              []int{0},
			},
		},
	}
}

func newDependencyDescriptorPerfPacket(
	frameNumber uint64,
	keyFrameNumber uint64,
	chainDiff int,
	structure *dd.FrameDependencyStructure,
	structureUpdated bool,
	activeDecodeTargets *uint32,
) *buffer.ExtPacket {
	return &buffer.ExtPacket{
		IsKeyFrame: structure != nil,
		DependencyDescriptor: &buffer.ExtDependencyDescriptor{
			Descriptor: &dd.DependencyDescriptor{
				FirstPacketInFrame: true,
				LastPacketInFrame:  true,
				FrameNumber:        uint16(frameNumber),
				FrameDependencies: &dd.FrameDependencyTemplate{
					SpatialId:               0,
					TemporalId:              0,
					DecodeTargetIndications: []dd.DecodeTargetIndication{dd.DecodeTargetRequired},
					ChainDiffs:              []int{chainDiff},
				},
				ActiveDecodeTargetsBitmask: activeDecodeTargets,
				AttachedStructure:          structure,
			},
			DecodeTargets: []buffer.DependencyDescriptorDecodeTarget{
				{Target: 0, Layer: buffer.VideoLayer{Spatial: 0, Temporal: 0}},
			},
			StructureUpdated:           structureUpdated,
			ActiveDecodeTargetsUpdated: activeDecodeTargets != nil,
			Integrity:                  true,
			ExtFrameNum:                frameNumber,
			ExtKeyFrameNum:             keyFrameNumber,
		},
		Packet: &rtp.Packet{Header: rtp.Header{SequenceNumber: uint16(frameNumber), SSRC: 1}},
	}
}

func requireDependencyDescriptorSelectedExtension(
	t *testing.T,
	selector *DependencyDescriptor,
	packet *buffer.ExtPacket,
	structure *dd.FrameDependencyStructure,
	activeDecodeTargets *uint32,
) []byte {
	t.Helper()

	result := selector.Select(packet, 0)
	if !result.IsSelected {
		t.Fatalf("frame %d was not selected", packet.DependencyDescriptor.ExtFrameNum)
	}
	if len(result.DependencyDescriptorExtension) == 0 {
		t.Fatalf("frame %d returned an empty extension", packet.DependencyDescriptor.ExtFrameNum)
	}

	expected := dependencyDescriptorMarshalOracle(t, packet.DependencyDescriptor.Descriptor, structure, activeDecodeTargets)
	if !bytes.Equal(result.DependencyDescriptorExtension, expected) {
		t.Fatalf("frame %d extension did not match the stateless marshal oracle", packet.DependencyDescriptor.ExtFrameNum)
	}
	return result.DependencyDescriptorExtension
}

func dependencyDescriptorMarshalOracle(
	t *testing.T,
	descriptor *dd.DependencyDescriptor,
	structure *dd.FrameDependencyStructure,
	activeDecodeTargets *uint32,
) []byte {
	t.Helper()

	expectedDescriptor := *descriptor
	if expectedDescriptor.AttachedStructure == nil {
		expectedDescriptor.ActiveDecodeTargetsBitmask = activeDecodeTargets
	}
	expected, err := (&dd.DependencyDescriptorExtension{
		Descriptor: &expectedDescriptor,
		Structure:  structure,
	}).Marshal()
	if err != nil {
		t.Fatalf("marshal oracle: %v", err)
	}
	return expected
}

func requireDependencyDescriptorSelectedExtensions(t *testing.T, selector *DependencyDescriptor, frames []*buffer.ExtPacket) []byte {
	t.Helper()

	var (
		structure            *dd.FrameDependencyStructure
		first                []byte
		selectedExtension    []byte
		selectedCount        int
		customFieldsSelected bool
	)
	for _, packet := range frames {
		descriptor := packet.DependencyDescriptor.Descriptor
		if descriptor.AttachedStructure != nil {
			structure = descriptor.AttachedStructure
			if structure.NumDecodeTargets < 2 || structure.NumChains < 2 || len(structure.Templates) < 2 {
				t.Fatalf("frame %d did not attach a multi-target, multi-chain, multi-template structure", packet.DependencyDescriptor.ExtFrameNum)
			}
		}

		result := selector.Select(packet, 0)
		if !result.IsSelected {
			if descriptor.AttachedStructure != nil {
				t.Fatalf("frame %d did not select an attached structure update", packet.DependencyDescriptor.ExtFrameNum)
			}
			continue
		}
		if structure == nil {
			t.Fatalf("frame %d selected without a dependency structure", packet.DependencyDescriptor.ExtFrameNum)
		}
		if len(result.DependencyDescriptorExtension) == 0 {
			t.Fatalf("frame %d returned an empty extension", packet.DependencyDescriptor.ExtFrameNum)
		}

		activeDecodeTargets := buffer.GetActiveDecodeTargetBitmask(selector.GetCurrent(), packet.DependencyDescriptor.DecodeTargets)
		expected := dependencyDescriptorMarshalOracle(t, descriptor, structure, activeDecodeTargets)
		if !bytes.Equal(result.DependencyDescriptorExtension, expected) {
			t.Fatalf("frame %d extension did not match the stateless marshal oracle", packet.DependencyDescriptor.ExtFrameNum)
		}

		if len(descriptor.FrameDependencies.FrameDiffs) != 0 {
			template := dependencyDescriptorTemplateForLayer(t, structure, descriptor.FrameDependencies)
			if slices.Equal(descriptor.FrameDependencies.DecodeTargetIndications, template.DecodeTargetIndications) ||
				slices.Equal(descriptor.FrameDependencies.FrameDiffs, template.FrameDiffs) ||
				slices.Equal(descriptor.FrameDependencies.ChainDiffs, template.ChainDiffs) {
				t.Fatalf("frame %d did not exercise custom DTI, frame-diff, and chain-diff fields", packet.DependencyDescriptor.ExtFrameNum)
			}
			customFieldsSelected = true
		}

		if first == nil {
			first = result.DependencyDescriptorExtension
		} else {
			requireDependencyDescriptorDistinctBacking(t, first, result.DependencyDescriptorExtension)
		}
		selectedExtension = result.DependencyDescriptorExtension
		selectedCount++
	}

	if selectedCount < 2 {
		t.Fatalf("expected at least two selected frames, got %d", selectedCount)
	}
	if !customFieldsSelected {
		t.Fatal("no selected frame exercised custom DTI, frame-diff, and chain-diff fields")
	}
	return selectedExtension
}

func dependencyDescriptorTemplateForLayer(t *testing.T, structure *dd.FrameDependencyStructure, frame *dd.FrameDependencyTemplate) *dd.FrameDependencyTemplate {
	t.Helper()

	for _, template := range structure.Templates {
		if template.SpatialId == frame.SpatialId && template.TemporalId == frame.TemporalId {
			return template
		}
	}
	t.Fatalf("no template found for spatial %d temporal %d", frame.SpatialId, frame.TemporalId)
	return nil
}

func requireDependencyDescriptorDistinctBacking(t *testing.T, first, second []byte) {
	t.Helper()

	if &first[0] == &second[0] {
		t.Fatal("successive selected extensions share backing storage")
	}
}
