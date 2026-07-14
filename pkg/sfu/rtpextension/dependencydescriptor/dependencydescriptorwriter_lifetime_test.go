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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDependencyDescriptorWriterResetBufReleasesPayload(t *testing.T) {
	structure := &FrameDependencyStructure{
		NumDecodeTargets: 1,
		Templates: []*FrameDependencyTemplate{
			{
				DecodeTargetIndications: []DecodeTargetIndication{DecodeTargetRequired},
				FrameDiffs:              []int{},
				ChainDiffs:              []int{},
			},
		},
	}
	descriptor := &DependencyDescriptor{
		FrameNumber:       1,
		FrameDependencies: structure.Templates[0].Clone(),
	}
	writer, err := NewDependencyDescriptorWriter(nil, structure, ^uint32(0), descriptor)
	require.NoError(t, err)

	payload := make([]byte, 4)
	writer.ResetBuf(payload)
	require.NoError(t, writer.Write())
	writer.ResetBuf(nil)
	require.Nil(t, writer.writer.buf)

	descriptor.FrameDependencies = &FrameDependencyTemplate{SpatialId: 1}
	writer.ResetBuf(payload)
	require.Error(t, writer.Write())
	writer.ResetBuf(nil)
	require.Nil(t, writer.writer.buf)
}
