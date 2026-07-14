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

package dependencydescriptor_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	dd "github.com/livekit/livekit-server/pkg/sfu/rtpextension/dependencydescriptor"
)

func TestDependencyDescriptorExtensionValueCopyAfterMarshal(t *testing.T) {
	structure := &dd.FrameDependencyStructure{
		NumDecodeTargets: 1,
		Templates: []*dd.FrameDependencyTemplate{
			{
				DecodeTargetIndications: []dd.DecodeTargetIndication{dd.DecodeTargetRequired},
				FrameDiffs:              []int{},
				ChainDiffs:              []int{},
			},
		},
	}
	descriptor := &dd.DependencyDescriptor{
		FrameNumber:       42,
		FrameDependencies: structure.Templates[0].Clone(),
	}
	extension := dd.DependencyDescriptorExtension{
		Descriptor: descriptor,
		Structure:  structure,
	}
	byValue := map[dd.DependencyDescriptorExtension]struct{}{extension: {}}
	require.Len(t, byValue, 1)

	first, err := extension.Marshal()
	require.NoError(t, err)
	firstSnapshot := append([]byte(nil), first...)
	copied := extension
	second, err := copied.Marshal()
	require.NoError(t, err)
	require.False(t, &first[0] == &second[0])
	require.Equal(t, firstSnapshot, first)
}
