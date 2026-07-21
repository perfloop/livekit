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

	"github.com/livekit/livekit-server/pkg/sfu/rtpextension/dependencydescriptor"
)

func TestDependencyDescriptorWriterMapKey(t *testing.T) {
	writer := dependencydescriptor.DependencyDescriptorWriter{}
	writers := map[dependencydescriptor.DependencyDescriptorWriter]struct{}{writer: {}}
	if _, ok := writers[writer]; !ok {
		t.Fatal("DependencyDescriptorWriter is not usable as a map key")
	}
}
