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
	activePayload := []byte{0x11, 0x22, 0x33}
	omittedPayload := []byte{0x51, 0x84, 0x13, 0xa7, 0x3e}
	if err := header.SetExtension(1, activePayload); err != nil {
		t.Fatal(err)
	}
	if err := header.SetExtension(2, omittedPayload); err != nil {
		t.Fatal(err)
	}
	if cap(header.Extensions) < 2 {
		t.Fatalf("header extension capacity = %d, want at least 2", cap(header.Extensions))
	}

	// Header.Extensions is exported. A caller can shorten it while a later
	// capacity slot still contains an extension payload.
	header.Extensions = header.Extensions[:1]
	packet := &Packet{
		Header:      header,
		HeaderPool:  headerPool,
		Payload:     []byte{0x01},
		WriteStream: headerPoolTestWriter{},
	}
	if _, err := NewBase(logger.GetLogger(), nil).SendPacket(packet); err != nil {
		t.Fatal(err)
	}

	pooled, ok := headerPool.Get().(*rtp.Header)
	if !ok || pooled == nil {
		t.Fatal("header pool did not return the sent header")
	}
	if pooled != header {
		t.Fatal("header pool returned a different header")
	}

	// Reslice through the public Header.Extensions field and use public RTP
	// operations to make any inactive retained descriptor observable.
	pooled.Extensions = pooled.Extensions[:cap(pooled.Extensions)]
	pooled.Extension = true
	pooled.ExtensionProfile = rtp.ExtensionProfileOneByte
	if got := pooled.GetExtension(2); got != nil {
		t.Fatalf("pooled header retained omitted extension payload = %x", got)
	}
	wire, err := pooled.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(wire, omittedPayload) {
		t.Fatalf("pooled header retained omitted extension bytes in serialized form: %x", wire)
	}
}
