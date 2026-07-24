// Copyright 2026 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package client

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/protocol/livekit"

	"github.com/livekit/livekit-server/pkg/rtc"
)

func TestRTCClientUnpacksTaggedUnlabeledBatch(t *testing.T) {
	records := [][]byte{[]byte("first"), nil, []byte("third")}
	encoded, err := rtc.MarshalUnlabeledBatch(records)
	if err != nil {
		t.Fatal(err)
	}
	topic := rtc.UnlabeledBatchTopic
	packet, err := proto.Marshal(&livekit.DataPacket{
		Value: &livekit.DataPacket_User{
			User: &livekit.UserPacket{
				Payload: encoded,
				Topic:   &topic,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	var received [][]byte
	client := &RTCClient{
		OnDataReceived: func(data []byte, _ string) {
			received = append(received, append([]byte(nil), data...))
		},
	}
	client.handleDataMessage(livekit.DataPacket_RELIABLE, packet)

	if len(received) != len(records) {
		t.Fatalf("received %d records, want %d", len(received), len(records))
	}
	for i := range records {
		if !bytes.Equal(received[i], records[i]) {
			t.Fatalf("record %d = %q, want %q", i, received[i], records[i])
		}
	}
}
