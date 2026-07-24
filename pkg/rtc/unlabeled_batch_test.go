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

package rtc

import (
	"bytes"
	"testing"
)

func TestUnlabeledBatchRoundTrip(t *testing.T) {
	records := make([][]byte, UnlabeledBatchMaxRecords)
	for i := range records {
		records[i] = bytes.Repeat([]byte{byte(i)}, i+1)
	}

	encoded, err := MarshalUnlabeledBatch(records)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalUnlabeledBatch(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(records) {
		t.Fatalf("decoded %d records, want %d", len(decoded), len(records))
	}
	for i := range records {
		if !bytes.Equal(decoded[i], records[i]) {
			t.Fatalf("record %d = %q, want %q", i, decoded[i], records[i])
		}
	}
}

func TestMarshalUnlabeledBatchRejectsTooManyRecords(t *testing.T) {
	records := make([][]byte, UnlabeledBatchMaxRecords+1)
	if _, err := MarshalUnlabeledBatch(records); err == nil {
		t.Fatal("expected oversized record count to fail")
	}
}
