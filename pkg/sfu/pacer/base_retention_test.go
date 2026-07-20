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

package pacer

import (
	"reflect"
	"sync"
	"testing"

	"github.com/livekit/protocol/logger"
	"github.com/pion/rtp"
)

type retainedHeaderTestWriter struct{}

func (retainedHeaderTestWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	return header.MarshalSize() + len(payload), nil
}

func (retainedHeaderTestWriter) Write(payload []byte) (int, error) {
	return len(payload), nil
}

func sendRetainedHeader(t *testing.T, header *rtp.Header) *rtp.Header {
	t.Helper()

	headerPool := &sync.Pool{}
	packet := PacketFactory.Get().(*Packet)
	*packet = Packet{
		Header:                 header,
		HeaderPool:             headerPool,
		RetainHeaderExtensions: true,
		HeaderSize:             header.MarshalSize(),
		Payload:                []byte{1},
		WriteStream:            retainedHeaderTestWriter{},
	}
	if _, err := NewBase(logger.GetLogger(), nil).SendPacket(packet); err != nil {
		t.Fatalf("send packet: %v", err)
	}

	reused, ok := headerPool.Get().(*rtp.Header)
	if !ok {
		t.Fatal("header pool returned an unexpected value")
	}
	return reused
}

func TestBaseSendPacketRetainedExtensionsClearPayloadReferences(t *testing.T) {
	header := &rtp.Header{
		Version:        2,
		PayloadType:    111,
		SequenceNumber: 1,
		Timestamp:      960,
		SSRC:           0x11223344,
		Extensions:     make([]rtp.Extension, 0, MaxRetainedHeaderExtensions),
	}
	for id := uint8(1); id <= MaxRetainedHeaderExtensions; id++ {
		if err := header.SetExtension(id, make([]byte, 17)); err != nil {
			t.Fatalf("set header extension %d: %v", id, err)
		}
	}

	reused := sendRetainedHeader(t, header)
	if len(reused.Extensions) != 0 {
		t.Fatalf("returned header has %d extensions, want 0", len(reused.Extensions))
	}
	if cap(reused.Extensions) != MaxRetainedHeaderExtensions {
		t.Fatalf("returned header extension capacity = %d, want %d", cap(reused.Extensions), MaxRetainedHeaderExtensions)
	}
	for i, extension := range reused.Extensions[:cap(reused.Extensions)] {
		payload := reflect.ValueOf(extension).FieldByName("payload")
		if payload.Kind() != reflect.Slice || !payload.IsNil() {
			t.Fatalf("returned header extension %d retains a payload reference", i)
		}
	}
}

func TestBaseSendPacketDropsShortenedExtensionCapacity(t *testing.T) {
	header := &rtp.Header{
		Version:        2,
		PayloadType:    111,
		SequenceNumber: 1,
		Timestamp:      960,
		SSRC:           0x11223344,
		Extensions:     make([]rtp.Extension, 0, MaxRetainedHeaderExtensions),
	}
	for id := uint8(1); id <= MaxRetainedHeaderExtensions; id++ {
		if err := header.SetExtension(id, make([]byte, 17)); err != nil {
			t.Fatalf("set header extension %d: %v", id, err)
		}
	}

	// A caller can shorten a previously populated slice; Base must not retain its hidden tail.
	header.Extensions = header.Extensions[:1]
	reused := sendRetainedHeader(t, header)
	if len(reused.Extensions) != 0 {
		t.Fatalf("returned header has %d extensions, want 0", len(reused.Extensions))
	}
	if cap(reused.Extensions) != 0 {
		t.Fatalf("returned header extension capacity = %d, want 0", cap(reused.Extensions))
	}
}

func TestBaseSendPacketDropsOversizedExtensionCapacity(t *testing.T) {
	header := &rtp.Header{
		Version:        2,
		PayloadType:    111,
		SequenceNumber: 1,
		Timestamp:      960,
		SSRC:           0x11223344,
		Extensions:     make([]rtp.Extension, 0, MaxRetainedHeaderExtensions+1),
	}
	if err := header.SetExtension(1, []byte{1}); err != nil {
		t.Fatalf("set header extension: %v", err)
	}

	reused := sendRetainedHeader(t, header)
	if len(reused.Extensions) != 0 {
		t.Fatalf("returned header has %d extensions, want 0", len(reused.Extensions))
	}
	if cap(reused.Extensions) != 0 {
		t.Fatalf("returned header extension capacity = %d, want 0", cap(reused.Extensions))
	}
}
