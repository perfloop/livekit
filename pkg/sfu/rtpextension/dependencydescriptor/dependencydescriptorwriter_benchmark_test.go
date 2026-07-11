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

	// 1. Test normal creation and Write
	buf := make([]byte, 1024)
	writer, err := NewDependencyDescriptorWriter(buf, structure, ^uint32(0), desc)
	if err != nil {
		t.Fatalf("NewDependencyDescriptorWriter failed: %v", err)
	}

	if err := writer.Write(); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// 2. Test descriptor mutation before Write (e.g. changing SpatialId or TemporalId)
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

	// Create writer with clone
	writer2, err := NewDependencyDescriptorWriter(buf, structure, ^uint32(0), &descClone)
	if err != nil {
		t.Fatalf("NewDependencyDescriptorWriter failed: %v", err)
	}

	// Mutate spatial and temporal ID before calling Write()
	descClone.FrameDependencies.SpatialId = targetSpatialId
	descClone.FrameDependencies.TemporalId = targetTemporalId

	// Write() must select the template matching the mutated spatial/temporal IDs!
	if err := writer2.Write(); err != nil {
		t.Fatalf("Write should have succeeded with mutated template: %v", err)
	}

	// Ensure the template used is the mutated one (bestTemplate.TemplateIdx spatial/temporal should match)
	selectedTemplate := structure.Templates[writer2.bestTemplate.TemplateIdx]
	if selectedTemplate.SpatialId != targetSpatialId || selectedTemplate.TemporalId != targetTemporalId {
		t.Errorf("template not updated after mutation! got spatial %d temporal %d, expected %d and %d",
			selectedTemplate.SpatialId, selectedTemplate.TemporalId, targetSpatialId, targetTemporalId)
	}

	// 3. Test mutation to an unsupported/invalid layer (no template found)
	descClone.FrameDependencies.SpatialId = 99
	descClone.FrameDependencies.TemporalId = 99

	// Write() must propagate the error correctly because the layer is unsupported!
	if err := writer2.Write(); err == nil {
		t.Errorf("expected template-not-found error, but got nil")
	} else {
		expectedError := "no template found for spatial layer 99 and temporal layer 99"
		if err.Error() != expectedError {
			t.Errorf("expected error %q, got %q", expectedError, err.Error())
		}
	}

	// 4. Test writer reuse via ResetBuf with mutated states
	descClone.FrameDependencies.SpatialId = originalSpatialId
	descClone.FrameDependencies.TemporalId = originalTemporalId

	newBuf := make([]byte, 1024)
	writer2.ResetBuf(newBuf)

	// Write should succeed now and use the restored valid template
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

	// Let's copy/clone the descriptor to avoid modifying the original globally
	descClone := *desc
	fdClone := *desc.FrameDependencies

	// Start with non-empty FrameDiffs
	fdClone.FrameDiffs = []int{1}
	descClone.FrameDependencies = &fdClone

	buf := make([]byte, 1024)
	writer, err := NewDependencyDescriptorWriter(buf, structure, ^uint32(0), &descClone)
	if err != nil {
		t.Fatalf("NewDependencyDescriptorWriter failed: %v", err)
	}

	// First Write
	if err := writer.Write(); err != nil {
		t.Fatalf("first Write failed: %v", err)
	}

	// Now mutate FrameDiffs to nil
	fdClone.FrameDiffs = nil

	// Reset buf and write - should invalidate cache because nilness changed!
	writer.ResetBuf(make([]byte, 1024))
	if err := writer.Write(); err != nil {
		t.Fatalf("second Write failed after mutating FrameDiffs to nil: %v", err)
	}

	// Check that cache successfully updated
	if (writer.nilBits & 1) == 0 {
		t.Errorf("expected nilBits to record FrameDiffs as nil")
	}

	// Mutate FrameDiffs back to empty slice []int{}
	fdClone.FrameDiffs = []int{}

	// Reset buf and write - should invalidate cache again because nilness changed from nil to empty slice!
	writer.ResetBuf(make([]byte, 1024))
	if err := writer.Write(); err != nil {
		t.Fatalf("third Write failed after mutating FrameDiffs to empty slice: %v", err)
	}

	if writer.nilBits&1 != 0 {
		t.Errorf("expected nilBits to record FrameDiffs as non-nil")
	}
}

func TestDependencyDescriptorWriterDifferential(t *testing.T) {
	// hex bytes from traffic capture
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

		// Now, marshal it back using active chains
		activeChains := ^uint32(0)

		// 1. Authoritative (non-cached) writing:
		authoritativeBytes, err := d.MarshalWithActiveChains(activeChains)
		if err != nil {
			t.Fatalf("[%d] MarshalWithActiveChains failed: %v", i, err)
		}

		// 2. Cached writing (testing writer reuse and ResetBuf):
		writer, err := NewDependencyDescriptorWriter(nil, d.Structure, activeChains, d.Descriptor)
		if err != nil {
			t.Fatalf("[%d] NewDependencyDescriptorWriter failed: %v", i, err)
		}

		// First write
		sizeBits := writer.ValueSizeBits()
		writeBuf := make([]byte, (sizeBits+7)/8)
		writer.ResetBuf(writeBuf)
		if err := writer.Write(); err != nil {
			t.Fatalf("[%d] first Write failed: %v", i, err)
		}

		// Verify first write matches authoritative
		if hex.EncodeToString(writeBuf) != hex.EncodeToString(authoritativeBytes) {
			t.Errorf("[%d] mismatch on first write! got %x, expected %x", i, writeBuf, authoritativeBytes)
		}

		// Trigger some dummy mutations to verify correct cache invalidation
		originalSpatialId := d.Descriptor.FrameDependencies.SpatialId
		d.Descriptor.FrameDependencies.SpatialId = 99 // mutate

		// ResetBuf and write - should trigger recalculation and fail with template-not-found error
		writer.ResetBuf(make([]byte, 1024))
		if err := writer.Write(); err == nil {
			t.Errorf("[%d] expected error on mutated spatial ID, got nil", i)
		}

		// Restore original spatial ID
		d.Descriptor.FrameDependencies.SpatialId = originalSpatialId

		// ResetBuf and write again - should succeed and match authoritative again
		writer.ResetBuf(writeBuf)
		if err := writer.Write(); err != nil {
			t.Fatalf("[%d] second Write failed: %v", i, err)
		}

		if hex.EncodeToString(writeBuf) != hex.EncodeToString(authoritativeBytes) {
			t.Errorf("[%d] mismatch on cached write! got %x, expected %x", i, writeBuf, authoritativeBytes)
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
