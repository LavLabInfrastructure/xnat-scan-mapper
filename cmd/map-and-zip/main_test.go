package main
package main

import "testing"

func TestCustomFieldsSupportsFlatAndRecordResponses(t *testing.T) {
	flat := map[string]any{"scanMap_test_t2ScanNumber": "7"}
	records := map[string]any{
		"ResultSet": map[string]any{
			"Result": []any{map[string]any{"field": "scanMap_test_t2ScanNumber", "value": "7"}},
		},
	}

	for _, payload := range []any{flat, records} {
		fields := customFields(payload)
		bundles := discoverBundles(fields, map[string]string{"t2ScanNumber": "T2"})
		if got := bundles["test"]["t2ScanNumber"]; got != "7" {
			t.Fatalf("expected scan 7, got %q", got)
		}
	}
}

func TestMappedFilename(t *testing.T) {
	tests := map[string]string{
		"nifti_a.nii.gz": "DWI_a.nii.gz",
		"nifti.bval":     "DWI.bval",
		"image.nii.gz":   "DWI.nii.gz",
		"image.bval":     "DWI.bval",
	}
	for input, expected := range tests {
		if got := mappedFilename(input, "DWI"); got != expected {
			t.Errorf("mappedFilename(%q) = %q, want %q", input, got, expected)
		}
	}
}