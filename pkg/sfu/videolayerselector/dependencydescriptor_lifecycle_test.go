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
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/protocol/logger"
)

const lifecycleFrameDiffCount = 2048

type lifecycleFrameDiffBacking [lifecycleFrameDiffCount]int

func TestDependencyDescriptorSelectorReleasesPacketFrameDependencies(t *testing.T) {
	for _, release := range []struct {
		name  string
		after func(*DependencyDescriptor, []*buffer.ExtPacket, int)
	}{
		{
			name: "next packet",
			after: func(selector *DependencyDescriptor, frames []*buffer.ExtPacket, index int) {
				selector.Select(frames[index+1], 0)
				frames[index+1] = nil
			},
		},
		{
			name: "restart",
			after: func(selector *DependencyDescriptor, _ []*buffer.ExtPacket, _ int) {
				selector.restart(1)
			},
		},
	} {
		t.Run(release.name, func(t *testing.T) {
			frames := createDDFrames(buffer.VideoLayer{Spatial: 2, Temporal: 2}, 1)
			selectedIndex := firstSelectedDependencyDescriptorFrame(t, frames)

			selector := NewDependencyDescriptor(logger.GetLogger())
			targetLayer := buffer.VideoLayer{Spatial: 2, Temporal: 2}
			selector.SetTarget(targetLayer)
			selector.SetRequestSpatial(targetLayer.Spatial)
			for index := 0; index < selectedIndex; index++ {
				selector.Select(frames[index], 0)
			}

			frame := frames[selectedIndex]
			diff := int(frame.DependencyDescriptor.ExtFrameNum - frames[0].DependencyDescriptor.ExtFrameNum)
			require.Positive(t, diff)
			finalized := attachFrameDiffBacking(frame, diff)
			result := selector.Select(frame, 0)
			require.True(t, result.IsSelected)
			require.Nil(t, selector.ddToMarshal.FrameDependencies)

			frames[selectedIndex] = nil
			frame = nil
			release.after(selector, frames, selectedIndex)
			frames = nil
			requireFrameDiffBackingFinalized(t, finalized)
		})
	}
}

func firstSelectedDependencyDescriptorFrame(t *testing.T, frames []*buffer.ExtPacket) int {
	t.Helper()

	selector := NewDependencyDescriptor(logger.GetLogger())
	targetLayer := buffer.VideoLayer{Spatial: 2, Temporal: 2}
	selector.SetTarget(targetLayer)
	selector.SetRequestSpatial(targetLayer.Spatial)
	require.True(t, selector.Select(frames[0], 0).IsSelected)
	for index, frame := range frames[1:] {
		if selector.Select(frame, 0).IsSelected {
			return index + 1
		}
	}
	t.Fatal("dependency descriptor selector did not select a non-key frame")
	return 0
}

func attachFrameDiffBacking(frame *buffer.ExtPacket, diff int) <-chan struct{} {
	finalized := make(chan struct{})
	backing := new(lifecycleFrameDiffBacking)
	for index := range backing[:] {
		backing[index] = diff
	}
	runtime.SetFinalizer(backing, func(*lifecycleFrameDiffBacking) {
		close(finalized)
	})
	frame.DependencyDescriptor.Descriptor.FrameDependencies.FrameDiffs = backing[:]
	return finalized
}

func requireFrameDiffBackingFinalized(t *testing.T, finalized <-chan struct{}) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		select {
		case <-finalized:
			return
		default:
		}
		runtime.Gosched()
	}
	t.Fatal("packet-owned frame dependency backing remained reachable")
}
