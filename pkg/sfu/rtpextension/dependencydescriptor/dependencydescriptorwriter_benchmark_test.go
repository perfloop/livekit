package dependencydescriptor

import (
	"encoding/hex"
	"fmt"
	"testing"
)

func getValidStructureAndDescriptor() (*FrameDependencyStructure, *DependencyDescriptor, error) {
	h := "c1017280081485214eafffaaaa863cf0430c10c302afc0aaa0063c00430010c002a000a80006000040001d954926e082b04a0941b820ac1282503157f974000ca864330e222222eca8655304224230eca877530077004200ef008601df010d"
	buf, err := hex.DecodeString(h)
	if err != nil {
		return nil, nil, err
	}
	var ddVal DependencyDescriptor
	var d = DependencyDescriptorExtension{
		Descriptor: &ddVal,
	}
	if _, err := d.Unmarshal(buf); err != nil {
		return nil, nil, err
	}
	if ddVal.AttachedStructure == nil {
		return nil, nil, fmt.Errorf("expected AttachedStructure")
	}
	return ddVal.AttachedStructure, &ddVal, nil
}

func TestDependencyDescriptorWriterLifecycle(t *testing.T) {
	structure, desc, err := getValidStructureAndDescriptor()
	if err != nil {
		t.Fatalf("failed to get valid structure and descriptor: %v", err)
	}

	buf := make([]byte, 1024)
	writer, err := NewDependencyDescriptorWriter(buf, structure, ^uint32(0), desc)
	if err != nil {
		t.Fatalf("NewDependencyDescriptorWriter failed: %v", err)
	}

	if err := writer.Write(); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// 1. Test descriptor mutation before Write (e.g. changing SpatialId or TemporalId)
	descClone := *desc
	fdClone := *desc.FrameDependencies
	descClone.FrameDependencies = &fdClone

	// Find another valid template from structure to mutate to
	var originalSpatialId = descClone.FrameDependencies.SpatialId
	var originalTemporalId = descClone.FrameDependencies.TemporalId
	var targetSpatialId = -1
	var targetTemporalId = -1

	for _, tmpl := range structure.Templates {
		if tmpl.SpatialId != originalSpatialId || tmpl.TemporalId != originalTemporalId {
			targetSpatialId = tmpl.SpatialId
			targetTemporalId = tmpl.TemporalId
			break
		}
	}

	if targetSpatialId == -1 {
		t.Fatalf("could not find a different valid template to mutate to")
	}

	writer2, err := NewDependencyDescriptorWriter(buf, structure, ^uint32(0), &descClone)
	if err != nil {
		t.Fatalf("NewDependencyDescriptorWriter failed: %v", err)
	}

	// Mutate spatial and temporal ID before calling Write()
	descClone.FrameDependencies.SpatialId = targetSpatialId
	descClone.FrameDependencies.TemporalId = targetTemporalId

	if err := writer2.Write(); err != nil {
		t.Fatalf("Write should have succeeded with mutated template: %v", err)
	}

	selectedTemplate := structure.Templates[writer2.bestTemplate.TemplateIdx]
	if selectedTemplate.SpatialId != targetSpatialId || selectedTemplate.TemporalId != targetTemporalId {
		t.Errorf("template not updated after mutation! got spatial %d temporal %d, expected %d and %d",
			selectedTemplate.SpatialId, selectedTemplate.TemporalId, targetSpatialId, targetTemporalId)
	}

	// 2. Test mutation to an unsupported/invalid layer (no template found)
	descClone.FrameDependencies.SpatialId = 99
	descClone.FrameDependencies.TemporalId = 99

	if err := writer2.Write(); err == nil {
		t.Errorf("expected template-not-found error, but got nil")
	} else {
		expectedError := "no template found for spatial layer 99 and temporal layer 99"
		if err.Error() != expectedError {
			t.Errorf("expected error %q, got %q", expectedError, err.Error())
		}
	}

	// 3. Test writer reuse via ResetBuf with mutated states
	descClone.FrameDependencies.SpatialId = originalSpatialId
	descClone.FrameDependencies.TemporalId = originalTemporalId

	newBuf := make([]byte, 1024)
	writer2.ResetBuf(newBuf)

	if err := writer2.Write(); err != nil {
		t.Fatalf("Write failed after ResetBuf and restoring valid template: %v", err)
	}

	selectedTemplate = structure.Templates[writer2.bestTemplate.TemplateIdx]
	if selectedTemplate.SpatialId != originalSpatialId || selectedTemplate.TemporalId != originalTemporalId {
		t.Errorf("template not updated after ResetBuf and restoring valid template! got spatial %d temporal %d",
			selectedTemplate.SpatialId, selectedTemplate.TemporalId)
	}
}

func TestDependencyDescriptorWriterNilToEmpty(t *testing.T) {
	structure, desc, err := getValidStructureAndDescriptor()
	if err != nil {
		t.Fatalf("failed to get valid structure and descriptor: %v", err)
	}

	descClone := *desc
	fdClone := *desc.FrameDependencies
	fdClone.FrameDiffs = []int{1}
	descClone.FrameDependencies = &fdClone

	// Base non-cached authoritative bytes
	extAuthoritative := DependencyDescriptorExtension{
		Structure:  structure,
		Descriptor: &descClone,
	}
	expectedBytes, err := extAuthoritative.MarshalWithActiveChains(^uint32(0))
	if err != nil {
		t.Fatalf("MarshalWithActiveChains failed: %v", err)
	}

	buf := make([]byte, 1024)
	writer, err := NewDependencyDescriptorWriter(buf, structure, ^uint32(0), &descClone)
	if err != nil {
		t.Fatalf("NewDependencyDescriptorWriter failed: %v", err)
	}

	// First Write
	sizeBits := writer.ValueSizeBits()
	writeBuf := make([]byte, (sizeBits+7)/8)
	writer.ResetBuf(writeBuf)
	if err := writer.Write(); err != nil {
		t.Fatalf("first Write failed: %v", err)
	}

	if hex.EncodeToString(writeBuf) != hex.EncodeToString(expectedBytes) {
		t.Fatalf("mismatch on first write: got %x, expected %x", writeBuf, expectedBytes)
	}

	// Mutate FrameDiffs to nil
	fdClone.FrameDiffs = nil
	expectedBytesNil, err := extAuthoritative.MarshalWithActiveChains(^uint32(0))
	if err != nil {
		t.Fatalf("MarshalWithActiveChains failed with nil FrameDiffs: %v", err)
	}

	sizeBitsNil := writer.ValueSizeBits()
	writeBufNil := make([]byte, (sizeBitsNil+7)/8)
	writer.ResetBuf(writeBufNil)
	if err := writer.Write(); err != nil {
		t.Fatalf("second Write failed with nil FrameDiffs: %v", err)
	}

	if hex.EncodeToString(writeBufNil) != hex.EncodeToString(expectedBytesNil) {
		t.Fatalf("mismatch on nil FrameDiffs: got %x, expected %x", writeBufNil, expectedBytesNil)
	}

	// Mutate FrameDiffs back to non-empty
	fdClone.FrameDiffs = []int{1}
	sizeBitsRestored := writer.ValueSizeBits()
	writeBufRestored := make([]byte, (sizeBitsRestored+7)/8)
	writer.ResetBuf(writeBufRestored)
	if err := writer.Write(); err != nil {
		t.Fatalf("third Write failed: %v", err)
	}

	if hex.EncodeToString(writeBufRestored) != hex.EncodeToString(expectedBytes) {
		t.Fatalf("mismatch on restored FrameDiffs: got %x, expected %x", writeBufRestored, expectedBytes)
	}
}

func TestDependencyDescriptorWriterInPlaceMutation(t *testing.T) {
	structure, desc, err := getValidStructureAndDescriptor()
	if err != nil {
		t.Fatalf("failed to get valid structure and descriptor: %v", err)
	}

	descClone := *desc
	fdClone := *desc.FrameDependencies
	fdClone.FrameDiffs = []int{1}
	descClone.FrameDependencies = &fdClone

	// Base non-cached authoritative bytes
	extAuthoritative := DependencyDescriptorExtension{
		Structure:  structure,
		Descriptor: &descClone,
	}
	expectedBytes, err := extAuthoritative.MarshalWithActiveChains(^uint32(0))
	if err != nil {
		t.Fatalf("MarshalWithActiveChains failed: %v", err)
	}

	buf := make([]byte, 1024)
	writer, err := NewDependencyDescriptorWriter(buf, structure, ^uint32(0), &descClone)
	if err != nil {
		t.Fatalf("NewDependencyDescriptorWriter failed: %v", err)
	}

	// First Write
	sizeBits := writer.ValueSizeBits()
	writeBuf := make([]byte, (sizeBits+7)/8)
	writer.ResetBuf(writeBuf)
	if err := writer.Write(); err != nil {
		t.Fatalf("first Write failed: %v", err)
	}

	if hex.EncodeToString(writeBuf) != hex.EncodeToString(expectedBytes) {
		t.Fatalf("mismatch: got %x, expected %x", writeBuf, expectedBytes)
	}

	// Mutate slice in-place: e.g. change fdClone.FrameDiffs[0] from 1 to 99
	fdClone.FrameDiffs[0] = 99
	expectedBytesMutated, err := extAuthoritative.MarshalWithActiveChains(^uint32(0))
	if err != nil {
		t.Fatalf("MarshalWithActiveChains failed: %v", err)
	}

	sizeBitsMutated := writer.ValueSizeBits()
	writeBufMutated := make([]byte, (sizeBitsMutated+7)/8)
	writer.ResetBuf(writeBufMutated)
	if err := writer.Write(); err != nil {
		t.Fatalf("second Write failed: %v", err)
	}

	if hex.EncodeToString(writeBufMutated) != hex.EncodeToString(expectedBytesMutated) {
		t.Fatalf("mismatch after in-place mutation: got %x, expected %x", writeBufMutated, expectedBytesMutated)
	}
}

func TestDependencyDescriptorWriterDifferential(t *testing.T) {
	hexes := []string{
		"c1017280081485214eafffaaaa863cf0430c10c302afc0aaa0063c00430010c002a000a80006000040001d954926e082b04a0941b820ac1282503157f974000ca864330e222222eca8655304224230eca877530077004200ef008601df010d",
		"86017340fc",
		"46017340fc",
		"c3017540fc",
		"88017640fc",
		"48017640fc",
		"c2017840fc",
		"c1017280081485214eafffaaaa863cf0430c10c302afc0aaa0063c00430010c002a000a80006000040001d954926e082b04a0941b820ac1282503157f974000ca864330e222222eca8655304224230eca877530077004200ef008601df010d",
		"860173",
		"460173",
		"8b0174",
		"0b0174",
		"0b0174",
		"c30175",
	}

	var structure *FrameDependencyStructure

	for i, h := range hexes {
		buf, err := hex.DecodeString(h)
		if err != nil {
			t.Fatal(err)
		}

		var ddVal DependencyDescriptor
		var d = DependencyDescriptorExtension{
			Structure:  structure,
			Descriptor: &ddVal,
		}
		if _, err := d.Unmarshal(buf); err != nil {
			t.Fatal(err)
		}
		if ddVal.AttachedStructure != nil {
			structure = ddVal.AttachedStructure
		}

		if d.Structure == nil && d.Descriptor.AttachedStructure != nil {
			d.Structure = d.Descriptor.AttachedStructure
		}

		activeChains := ^uint32(0)

		// Authoritative
		authoritativeBytes, err := d.MarshalWithActiveChains(activeChains)
		if err != nil {
			t.Fatalf("[%d] MarshalWithActiveChains failed: %v", i, err)
		}

		// Cached
		writer, err := NewDependencyDescriptorWriter(nil, d.Structure, activeChains, d.Descriptor)
		if err != nil {
			t.Fatalf("[%d] NewDependencyDescriptorWriter failed: %v", i, err)
		}

		sizeBits := writer.ValueSizeBits()
		writeBuf := make([]byte, (sizeBits+7)/8)
		writer.ResetBuf(writeBuf)
		if err := writer.Write(); err != nil {
			t.Fatalf("[%d] first Write failed: %v", i, err)
		}

		if hex.EncodeToString(writeBuf) != hex.EncodeToString(authoritativeBytes) {
			t.Errorf("[%d] mismatch: got %x, expected %x", i, writeBuf, authoritativeBytes)
		}
	}
}

func BenchmarkDependencyDescriptorWriterWrite(b *testing.B) {
	structure, desc, err := getValidStructureAndDescriptor()
	if err != nil {
		b.Fatalf("failed to get valid structure and descriptor: %v", err)
	}

	buf := make([]byte, 1024)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		writer, err := NewDependencyDescriptorWriter(buf, structure, ^uint32(0), desc)
		if err != nil {
			b.Fatalf("NewDependencyDescriptorWriter failed: %v", err)
		}
		if err := writer.Write(); err != nil {
			b.Fatalf("Write failed: %v", err)
		}
	}
}
