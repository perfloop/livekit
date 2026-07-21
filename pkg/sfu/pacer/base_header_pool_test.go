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
	"bytes"
	"sync"
	"testing"

	"github.com/livekit/protocol/logger"
	"github.com/pion/rtp"
)

type headerPoolTestWriter struct{}

func (headerPoolTestWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	return header.MarshalSize() + len(payload), nil
}

func (headerPoolTestWriter) Write(payload []byte) (int, error) {
	return len(payload), nil
}

func TestBaseSendPacketClearsRetainedHeaderExtensionCapacity(t *testing.T) {
	headerPool := &sync.Pool{}
	header := &rtp.Header{
		Version:     2,
		PayloadType: 111,
		SSRC:        56,
	}
	stalePayload := []byte{0x51, 0x84, 0x13, 0xa7, 0x3e}
	for id := uint8(1); id <= 5; id++ {
		payload := []byte{id}
		if id == 5 {
			payload = stalePayload
		}
		if err := header.SetExtension(id, payload); err != nil {
			t.Fatal(err)
		}
	}
	if cap(header.Extensions) < 5 {
		t.Fatalf("header extension capacity = %d, want at least 5", cap(header.Extensions))
	}

	// Mirror a pooled DownTrack header after a five-extension packet followed
	// by a packet that needs only abs-send-time and transport-wide extensions.
	*header = rtp.Header{
		Version:     2,
		PayloadType: 111,
		SSRC:        56,
		Extensions:  header.Extensions[:0],
	}
	if err := header.SetExtension(1, []byte{0x11, 0x22, 0x33}); err != nil {
		t.Fatal(err)
	}
	if err := header.SetExtension(2, []byte{0x44, 0x55}); err != nil {
		t.Fatal(err)
	}

	packet := &Packet{
		Header:      header,
		HeaderPool:  headerPool,
		Payload:     []byte{0x01},
		WriteStream: headerPoolTestWriter{},
	}
	if _, err := NewBase(logger.GetLogger(), nil).SendPacket(packet); err != nil {
		t.Fatal(err)
	}
	if got := header.GetExtensionIDs(); len(got) != 0 {
		t.Fatalf("header extensions after return = %v, want none", got)
	}

	// The test retains the local header pointer, so it does not depend on
	// sync.Pool returning a value. Public RTP operations make any descriptor
	// left in the retained backing capacity observable.
	header.Extensions = header.Extensions[:cap(header.Extensions)]
	header.Extension = true
	header.ExtensionProfile = rtp.ExtensionProfileOneByte
	if got := header.GetExtension(5); got != nil {
		t.Fatalf("pooled header retained stale extension payload = %x", got)
	}
	wire, err := header.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(wire, stalePayload) {
		t.Fatalf("pooled header retained stale extension bytes in serialized form: %x", wire)
	}
}
