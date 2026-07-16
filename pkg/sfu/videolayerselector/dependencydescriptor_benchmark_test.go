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

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/protocol/logger"
)

var benchmarkDependencyDescriptorSelectSink byte

func BenchmarkDependencyDescriptorSelect(b *testing.B) {
	selector := NewDependencyDescriptor(logger.GetLogger())
	targetLayer := buffer.VideoLayer{}
	selector.SetTarget(targetLayer)
	selector.SetRequestSpatial(targetLayer.Spatial)

	frames := createDDFrames(targetLayer, 1)
	if result := selector.Select(frames[0], 0); !result.IsSelected {
		b.Fatal("key frame was not selected")
	}

	packet := frames[1]
	descriptor := packet.DependencyDescriptor.Descriptor

	b.ReportAllocs()
	b.ResetTimer()

	var sink byte
	for i := 0; i < b.N; i++ {
		frameNumber := uint16(i + 2)
		descriptor.FrameNumber = frameNumber
		packet.DependencyDescriptor.ExtFrameNum = uint64(i + 2)

		result := selector.Select(packet, 0)
		if !result.IsSelected || len(result.DependencyDescriptorExtension) == 0 {
			b.Fatal("advancing frame was not selected and marshaled")
		}
		sink ^= result.DependencyDescriptorExtension[len(result.DependencyDescriptorExtension)-1]
	}

	benchmarkDependencyDescriptorSelectSink = sink
}
