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

func requireDependencyDescriptorDistinctBacking(t *testing.T, first, second []byte) {
	t.Helper()

	if &first[0] == &second[0] {
		t.Fatal("successive selected extensions share backing storage")
	}
}
