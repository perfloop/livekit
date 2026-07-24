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

package test

import (
	"bytes"
	"testing"
	"time"

	testclient "github.com/livekit/livekit-server/test/client"
)

func TestUnlabeledDataDelivery(t *testing.T) {
	testUnlabeledDataDelivery(t, nil, nil)
}

func waitUntilUnlabeledDataConnected(t *testing.T, clients ...*testclient.RTCClient) {
	t.Helper()
	for _, client := range clients {
		if err := client.WaitUntilConnected(10 * time.Second); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUnlabeledDataLegacyDelivery(t *testing.T) {
	testUnlabeledDataDelivery(t, legacyUnlabeledClientOptions(), legacyUnlabeledClientOptions())
}

func TestUnlabeledDataOrderedDelivery(t *testing.T) {
	_, finish := setupSingleNodeTest(t.Name())
	defer finish()

	publisher := createRTCClient("unlabeled-ordered-publisher", defaultServerPort, testRTCServicePathv0, unlabeledDataClientOptions(nil))
	recipient := createRTCClient("unlabeled-ordered-recipient", defaultServerPort, testRTCServicePathv0, unlabeledDataClientOptions(nil))
	defer publisher.Stop()
	defer recipient.Stop()

	payloads := [][]byte{
		[]byte("unlabeled-record-000"),
		[]byte("unlabeled-record-001"),
		[]byte("unlabeled-record-002"),
	}
	received := make(chan []byte, len(payloads))
	recipient.OnDataReceived = func(data []byte, _ string) {
		received <- append([]byte(nil), data...)
	}
	waitUntilUnlabeledDataConnected(t, publisher, recipient)
	payload := make([]byte, len(payloads[0]))
	for _, want := range payloads {
		copy(payload, want)
		if err := publisher.PublishDataUnlabeled(payload); err != nil {
			t.Fatal(err)
		}
	}

	for i, want := range payloads {
		select {
		case got := <-received:
			if !bytes.Equal(got, want) {
				t.Fatalf("payload %d = %q, want %q", i, got, want)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("did not receive unlabeled payload %d", i)
		}
	}
}

func testUnlabeledDataDelivery(t *testing.T, publisherOptions, recipientOptions *testclient.Options) {
	t.Helper()
	_, finish := setupSingleNodeTest(t.Name())
	defer finish()

	publisher := createRTCClient("unlabeled-publisher", defaultServerPort, testRTCServicePathv0, unlabeledDataClientOptions(publisherOptions))
	recipient := createRTCClient("unlabeled-recipient", defaultServerPort, testRTCServicePathv0, unlabeledDataClientOptions(recipientOptions))
	defer publisher.Stop()
	defer recipient.Stop()

	payload := []byte("unlabeled-data-payload")
	received := make(chan []byte, 1)
	recipient.OnDataReceived = func(data []byte, _ string) {
		received <- append([]byte(nil), data...)
	}
	waitUntilUnlabeledDataConnected(t, publisher, recipient)
	if err := publisher.PublishDataUnlabeled(payload); err != nil {
		t.Fatal(err)
	}

	select {
	case data := <-received:
		if !bytes.Equal(data, payload) {
			t.Fatalf("payload = %q, want %q", data, payload)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("did not receive unlabeled data")
	}
}
