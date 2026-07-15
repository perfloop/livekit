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

type dependencyDescriptorSelectWorkload struct {
	selector             *DependencyDescriptor
	packet               *buffer.ExtPacket
	extensionDescriptor  *buffer.ExtDependencyDescriptor
	dependencyDescriptor *dd.DependencyDescriptor
	structure            *dd.FrameDependencyStructure
	activeDecodeTargets  uint32
	frame                uint64
}

func newDependencyDescriptorSelectWorkload(tb testing.TB) *dependencyDescriptorSelectWorkload {
	tb.Helper()

	const firstFrame = uint64(1)

	frameDependencies := &dd.FrameDependencyTemplate{
		DecodeTargetIndications: []dd.DecodeTargetIndication{dd.DecodeTargetRequired},
	}
	structure := &dd.FrameDependencyStructure{
		StructureId:      0,
		NumDecodeTargets: 1,
		Templates:        []*dd.FrameDependencyTemplate{frameDependencies},
	}
	workload := &dependencyDescriptorSelectWorkload{
		selector:            NewDependencyDescriptor(logger.GetLogger()),
		structure:           structure,
		activeDecodeTargets: 1,
		frame:               firstFrame,
	}
	workload.dependencyDescriptor = &dd.DependencyDescriptor{
		FirstPacketInFrame:         true,
		LastPacketInFrame:          true,
		FrameNumber:                uint16(firstFrame),
		FrameDependencies:          frameDependencies,
		ActiveDecodeTargetsBitmask: &workload.activeDecodeTargets,
		AttachedStructure:          structure,
	}
	workload.extensionDescriptor = &buffer.ExtDependencyDescriptor{
		Descriptor: workload.dependencyDescriptor,
		DecodeTargets: []buffer.DependencyDescriptorDecodeTarget{{
			Target: 0,
			Layer:  buffer.VideoLayer{},
		}},
		StructureUpdated:           true,
		ActiveDecodeTargetsUpdated: true,
		Integrity:                  true,
		ExtFrameNum:                firstFrame,
		ExtKeyFrameNum:             firstFrame,
	}
	workload.packet = &buffer.ExtPacket{
		IsKeyFrame:           true,
		DependencyDescriptor: workload.extensionDescriptor,
		Packet:               &rtp.Packet{Header: rtp.Header{SequenceNumber: uint16(firstFrame), SSRC: 1}},
	}
	workload.selector.SetTarget(buffer.VideoLayer{})
	workload.selector.SetRequestSpatial(0)

	return workload
}

func (w *dependencyDescriptorSelectWorkload) selectPacket() VideoLayerSelectorResult {
	return w.selector.Select(w.packet, 0)
}

func (w *dependencyDescriptorSelectWorkload) advancePacket() {
	w.frame++
	w.packet.IsKeyFrame = false
	w.packet.Packet.SequenceNumber = uint16(w.frame)
	w.dependencyDescriptor.FrameNumber = uint16(w.frame)
	w.dependencyDescriptor.AttachedStructure = nil
	w.dependencyDescriptor.ActiveDecodeTargetsBitmask = nil
	w.extensionDescriptor.StructureUpdated = false
	w.extensionDescriptor.ActiveDecodeTargetsUpdated = false
	w.extensionDescriptor.ExtFrameNum = w.frame
}

func (w *dependencyDescriptorSelectWorkload) refreshStructure() {
	w.frame++
	w.packet.IsKeyFrame = true
	w.packet.Packet.SequenceNumber = uint16(w.frame)

	frameDependencies := &dd.FrameDependencyTemplate{
		DecodeTargetIndications: []dd.DecodeTargetIndication{dd.DecodeTargetSwitch},
	}
	w.structure = &dd.FrameDependencyStructure{
		StructureId:      1,
		NumDecodeTargets: 1,
		Templates:        []*dd.FrameDependencyTemplate{frameDependencies},
	}
	w.dependencyDescriptor.FrameNumber = uint16(w.frame)
	w.dependencyDescriptor.FrameDependencies = frameDependencies
	w.dependencyDescriptor.ActiveDecodeTargetsBitmask = &w.activeDecodeTargets
	w.dependencyDescriptor.AttachedStructure = w.structure
	w.extensionDescriptor.StructureUpdated = true
	w.extensionDescriptor.ActiveDecodeTargetsUpdated = true
	w.extensionDescriptor.ExtFrameNum = w.frame
	w.extensionDescriptor.ExtKeyFrameNum = w.frame
}

func (w *dependencyDescriptorSelectWorkload) marshalOracle(tb testing.TB) []byte {
	tb.Helper()

	descriptor := *w.dependencyDescriptor
	if descriptor.AttachedStructure == nil {
		descriptor.ActiveDecodeTargetsBitmask = &w.activeDecodeTargets
	}
	marshaled, err := (&dd.DependencyDescriptorExtension{
		Descriptor: &descriptor,
		Structure:  w.structure,
	}).Marshal()
	if err != nil {
		tb.Fatalf("marshal dependency descriptor oracle: %v", err)
	}
	return marshaled
}

func requireSelectedDependencyDescriptor(t *testing.T, result VideoLayerSelectorResult, expected []byte) []byte {
	t.Helper()
	if !result.IsSelected {
		t.Fatal("dependency descriptor was not selected")
	}
	if !bytes.Equal(expected, result.DependencyDescriptorExtension) {
		t.Fatalf("dependency descriptor bytes = %x, want %x", result.DependencyDescriptorExtension, expected)
	}
	return result.DependencyDescriptorExtension
}

func TestDependencyDescriptorSelectSequentialMarshal(t *testing.T) {
	workload := newDependencyDescriptorSelectWorkload(t)

	first := requireSelectedDependencyDescriptor(t, workload.selectPacket(), workload.marshalOracle(t))
	firstCopy := append([]byte(nil), first...)

	workload.advancePacket()
	second := requireSelectedDependencyDescriptor(t, workload.selectPacket(), workload.marshalOracle(t))
	if bytes.Equal(first, second) {
		t.Fatalf("successive dependency descriptor bytes are equal: %x", first)
	}

	workload.refreshStructure()
	refreshed := requireSelectedDependencyDescriptor(t, workload.selectPacket(), workload.marshalOracle(t))
	if bytes.Equal(second, refreshed) {
		t.Fatalf("dependency descriptor bytes did not change after structure refresh: %x", second)
	}
	if !bytes.Equal(first, firstCopy) {
		t.Fatalf("first dependency descriptor bytes changed after later selection: got %x, want %x", first, firstCopy)
	}
	if len(first) > 0 && &first[0] == &refreshed[0] {
		t.Fatal("successive selections returned the same dependency descriptor backing storage")
	}
}

func BenchmarkDependencyDescriptorSelect(b *testing.B) {
	workload := newDependencyDescriptorSelectWorkload(b)
	if result := workload.selectPacket(); !result.IsSelected || len(result.DependencyDescriptorExtension) == 0 {
		b.Fatal("initial dependency descriptor was not selected")
	}

	var checksum uint64
	for b.Loop() {
		workload.advancePacket()
		result := workload.selectPacket()
		if !result.IsSelected || len(result.DependencyDescriptorExtension) == 0 {
			b.Fatal("dependency descriptor was not selected")
		}
		checksum += uint64(result.DependencyDescriptorExtension[0])
	}
	if checksum == 0 {
		b.Fatal("dependency descriptor output was not consumed")
	}
}
