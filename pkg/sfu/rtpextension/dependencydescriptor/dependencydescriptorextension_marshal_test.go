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
	"bytes"
	"encoding/hex"
	"testing"
)

const dependencyDescriptorMarshalStructure = "c1017280081485214eafffaaaa863cf0430c10c302afc0aaa0063c00430010c002a000a80006000040001d954926e082b04a0941b820ac1282503157f974000ca864330e222222eca8655304224230eca877530077004200ef008601df010d"

func newDependencyDescriptorMarshalExtension(tb testing.TB) *DependencyDescriptorExtension {
	tb.Helper()

	buf, err := hex.DecodeString(dependencyDescriptorMarshalStructure)
	if err != nil {
		tb.Fatal(err)
	}

	descriptor := DependencyDescriptor{}
	parser := DependencyDescriptorExtension{Descriptor: &descriptor}
	if _, err = parser.Unmarshal(buf); err != nil {
		tb.Fatal(err)
	}
	if descriptor.AttachedStructure == nil {
		tb.Fatal("fixture did not contain a dependency structure")
	}

	structure := descriptor.AttachedStructure
	descriptor.AttachedStructure = nil
	descriptor.ActiveDecodeTargetsBitmask = nil

	return &DependencyDescriptorExtension{
		Descriptor: &descriptor,
		Structure:  structure,
	}
}

func TestDependencyDescriptorMarshal(t *testing.T) {
	extension := newDependencyDescriptorMarshalExtension(t)

	first, err := extension.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	firstCopy := append([]byte(nil), first...)

	extension.Descriptor.FrameNumber++
	second, err := extension.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, firstCopy) {
		t.Fatal("a later marshal modified a previously returned buffer")
	}

	decoded := DependencyDescriptor{}
	parser := DependencyDescriptorExtension{
		Descriptor: &decoded,
		Structure:  extension.Structure,
	}
	if _, err = parser.Unmarshal(second); err != nil {
		t.Fatal(err)
	}
	if decoded.FrameNumber != extension.Descriptor.FrameNumber {
		t.Fatalf("frame number = %d, want %d", decoded.FrameNumber, extension.Descriptor.FrameNumber)
	}
}

func BenchmarkDependencyDescriptorMarshalSteadyState(b *testing.B) {
	extension := newDependencyDescriptorMarshalExtension(b)
	frameNumber := extension.Descriptor.FrameNumber
	var buf []byte

	for b.Loop() {
		extension.Descriptor.FrameNumber = frameNumber
		frameNumber++

		var err error
		buf, err = extension.Marshal()
		if err != nil {
			b.Fatal(err)
		}
	}
	if len(buf) == 0 {
		b.Fatal("marshal returned an empty dependency descriptor")
	}
}
