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
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	dd "github.com/livekit/livekit-server/pkg/sfu/rtpextension/dependencydescriptor"
	"github.com/livekit/protocol/logger"
)

func TestDependencyDescriptorSelectKeepsMarshaledOutputIndependent(t *testing.T) {
	selector := NewDependencyDescriptor(logger.GetLogger())
	targetLayer := buffer.VideoLayer{}
	selector.SetTarget(targetLayer)
	selector.SetRequestSpatial(targetLayer.Spatial)

	frames := createDDFrames(targetLayer, 1)
	initial := selector.Select(frames[0], 0)
	require.True(t, initial.IsSelected)

	first := selector.Select(frames[1], 0)
	require.True(t, first.IsSelected)
	require.NotEmpty(t, first.DependencyDescriptorExtension)
	firstBytes := append([]byte(nil), first.DependencyDescriptorExtension...)

	second := selector.Select(frames[2], 0)
	require.True(t, second.IsSelected)
	require.NotEmpty(t, second.DependencyDescriptorExtension)

	require.Equal(t, firstBytes, first.DependencyDescriptorExtension)
	require.NotEqual(t, first.DependencyDescriptorExtension, second.DependencyDescriptorExtension)

	decoded := &dd.DependencyDescriptorExtension{
		Descriptor: &dd.DependencyDescriptor{},
		Structure:  frames[0].DependencyDescriptor.Descriptor.AttachedStructure,
	}
	_, err := decoded.Unmarshal(first.DependencyDescriptorExtension)
	require.NoError(t, err)
	require.Equal(t, frames[1].DependencyDescriptor.Descriptor.FrameNumber, decoded.Descriptor.FrameNumber)
}
