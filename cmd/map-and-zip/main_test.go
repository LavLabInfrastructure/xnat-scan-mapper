package main

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestBundleFingerprintTracksAddedAndRemovedFiles(t *testing.T) {
	sessionDir := t.TempDir()
	resourceDir := filepath.Join(sessionDir, "SCANS", "7", "NIFTI")
	if err := os.MkdirAll(resourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourceDir, "nifti.nii.gz"), []byte("nifti"), 0o644); err != nil {
		t.Fatal(err)
	}
	formValues := map[string]string{"t2ScanNumber": "7"}
	mapping := map[string]string{"t2ScanNumber": "T2"}
	initial, err := bundleFingerprint(sessionDir, formValues, mapping)
	if err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(sessionDir, "RESOURCES", "mapped_sessions", "session_bundle.zip")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		t.Fatal(err)
	}
	stagingDir := filepath.Join(sessionDir, "staging")
	if _, err := copyResource(resourceDir, filepath.Join(stagingDir, "T2"), "T2"); err != nil {
		t.Fatal(err)
	}
	if err := zipDirectory(stagingDir, archivePath, initial); err != nil {
		t.Fatal(err)
	}
	found, exists, err := existingBundleFingerprint(sessionDir, "session_bundle.zip")
	if err != nil || !exists || found != initial {
		t.Fatalf("existing archive fingerprint = %q, %t, %v; want %q, true, nil", found, exists, err, initial)
	}

	bvalPath := filepath.Join(resourceDir, "nifti.bval")
	if err := os.WriteFile(bvalPath, []byte("bval"), 0o644); err != nil {
		t.Fatal(err)
	}
	withBval, err := bundleFingerprint(sessionDir, formValues, mapping)
	if err != nil || withBval == initial {
		t.Fatalf("adding bval did not change fingerprint: %q, %v", withBval, err)
	}
	if err := os.Remove(bvalPath); err != nil {
		t.Fatal(err)
	}
	afterRemoval, err := bundleFingerprint(sessionDir, formValues, mapping)
	if err != nil || afterRemoval != initial {
		t.Fatalf("removing bval did not restore fingerprint: %q, %v", afterRemoval, err)
	}
}
