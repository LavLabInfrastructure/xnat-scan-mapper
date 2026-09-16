package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const scanMapFieldPrefix = "scanMap_"

var unsafeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

type mappings []string

func (values *mappings) String() string { return strings.Join(*values, ", ") }

func (values *mappings) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func parseMappings(values mappings) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for _, value := range values {
		parts := strings.SplitN(value, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(parts[1], `/\`) {
			return nil, fmt.Errorf("invalid mapping %q; use formKey:destination", value)
		}
		result[parts[0]] = parts[1]
	}
	return result, nil
}

func fetchJSON(host, path, username, password string) (any, error) {
	requestURL, err := url.JoinPath(strings.TrimRight(host, "/"), path)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	request.SetBasicAuth(username, password)
	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to XNAT at %q: %w", host, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("XNAT returned HTTP %d for %s", response.StatusCode, requestURL)
	}
	var payload any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode XNAT response: %w", err)
	}
	return payload, nil
}

func customFields(payload any) map[string]string {
	fields := map[string]string{}
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			fieldName := stringValue(value["field"])
			if fieldName == "" {
				fieldName = stringValue(value["fieldName"])
			}
			if fieldName == "" {
				fieldName = stringValue(value["name"])
			}
			if fieldName == "" {
				fieldName = stringValue(value["key"])
			}
			if fieldName != "" && isScalar(value["value"]) {
				fields[fieldName] = stringValue(value["value"])
			}
			for key, child := range value {
				if key != "field" && key != "fieldName" && key != "name" && key != "key" && key != "value" && isScalar(child) {
					fields[key] = stringValue(child)
				}
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		case string:
			trimmed := strings.TrimSpace(value)
			if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
				var nested any
				if json.Unmarshal([]byte(trimmed), &nested) == nil {
					walk(nested)
				}
			}
		}
	}
	walk(payload)
	return fields
}

func isScalar(value any) bool {
	switch value.(type) {
	case string, float64, bool:
		return true
	default:
		return false
	}
}

func stringValue(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case float64:
		return fmt.Sprintf("%v", value)
	case bool:
		return fmt.Sprintf("%t", value)
	default:
		return ""
	}
}

func discoverBundles(fields, mapping map[string]string) map[string]map[string]string {
	bundles := map[string]map[string]string{}
	for fieldName, fieldValue := range fields {
		if !strings.HasPrefix(fieldName, scanMapFieldPrefix) {
			continue
		}
		for mappingKey := range mapping {
			suffix := "_" + mappingKey
			if !strings.HasSuffix(fieldName, suffix) {
				continue
			}
			bundleName := strings.TrimSuffix(strings.TrimPrefix(fieldName, scanMapFieldPrefix), suffix)
			if bundleName != "" && !strings.ContainsAny(bundleName, `/\`) {
				if bundles[bundleName] == nil {
					bundles[bundleName] = map[string]string{}
				}
				bundles[bundleName][mappingKey] = fieldValue
			}
			break
		}
	}
	return bundles
}

func findNiftiResource(scanDir string) (string, error) {
	var resource string
	err := filepath.WalkDir(scanDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && strings.EqualFold(entry.Name(), "NIFTI") {
			resource = path
			return filepath.SkipDir
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return resource, err
}

func mappedFilename(filename, label string) string {
	if strings.HasPrefix(filename, "nifti") {
		return label + strings.TrimPrefix(filename, "nifti")
	}
	if strings.HasSuffix(filename, ".nii.gz") {
		return label + ".nii.gz"
	}
	extension := filepath.Ext(filename)
	if extension == "" {
		return filename
	}
	return label + extension
}

func copyResource(resourceDir, destinationDir, label string) (int, error) {
	count := 0
	err := filepath.WalkDir(resourceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relativePath, err := filepath.Rel(resourceDir, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(destinationDir, relativePath)
		if filepath.Dir(relativePath) == "." {
			targetPath = filepath.Join(destinationDir, mappedFilename(entry.Name(), label))
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		if _, err := os.Stat(targetPath); err == nil {
			return fmt.Errorf("multiple files map to %s", targetPath)
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		defer source.Close()
		target, err := os.Create(targetPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(target, source); err != nil {
			target.Close()
			return err
		}
		if err := target.Close(); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func zipDirectory(sourceDir, zipPath string) error {
	archive, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer archive.Close()
	writer := zip.NewWriter(archive)
	defer writer.Close()
	return filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relativePath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		entryWriter, err := writer.Create(filepath.ToSlash(relativePath))
		if err != nil {
			return err
		}
		_, err = io.Copy(entryWriter, file)
		return err
	})
}

func main() {
	var sessionDir, outputDir, sessionID, sessionLabel string
	var mappingArgs mappings
	flag.StringVar(&sessionDir, "session-dir", "", "Mounted session input directory")
	flag.StringVar(&outputDir, "output-dir", "", "Directory for generated bundles")
	flag.StringVar(&sessionID, "session-id", "", "XNAT experiment ID")
	flag.StringVar(&sessionLabel, "session-label", "", "XNAT session label for archive names")
	flag.Var(&mappingArgs, "map", "Map a form field to a bundle directory; repeat as formKey:destination")
	flag.Parse()
	if sessionDir == "" || outputDir == "" || sessionID == "" || sessionLabel == "" || len(mappingArgs) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	mapping, err := parseMappings(mappingArgs)
	if err != nil {
		fatal(err)
	}
	host := os.Getenv("XNAT_API_HOST")
	if host == "" {
		host = os.Getenv("XNAT_HOST")
	}
	username, password := os.Getenv("XNAT_USER"), os.Getenv("XNAT_PASS")
	if host == "" || username == "" || password == "" {
		fatal(errors.New("XNAT_API_HOST or XNAT_HOST, XNAT_USER, and XNAT_PASS must be set"))
	}
	payload, err := fetchJSON(host, "xapi/custom-fields/experiments/"+sessionID+"/fields", username, password)
	if err != nil {
		fatal(err)
	}
	bundles := discoverBundles(customFields(payload), mapping)
	if len(bundles) == 0 {
		fatal(errors.New("no scanMap_<bundle>_<field-key> values were found in the Custom Fields API response"))
	}
	safeSessionLabel := strings.Trim(unsafeFilenameChars.ReplaceAllString(strings.TrimSpace(sessionLabel), "_"), "._")
	if safeSessionLabel == "" {
		safeSessionLabel = sessionID
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		fatal(err)
	}
	for bundleName, formValues := range bundles {
		stagingDir := filepath.Join(outputDir, "_"+bundleName)
		if err := os.RemoveAll(stagingDir); err != nil {
			fatal(err)
		}
		if err := os.MkdirAll(stagingDir, 0o755); err != nil {
			fatal(err)
		}
		copied := 0
		for formKey, destination := range mapping {
			scanNumber := strings.TrimSpace(formValues[formKey])
			if scanNumber == "" || scanNumber == "-1" {
				continue
			}
			resource, err := findNiftiResource(filepath.Join(sessionDir, "SCANS", scanNumber))
			if err != nil {
				fatal(err)
			}
			if resource == "" {
				fmt.Fprintf(os.Stderr, "warning: no NIFTI resource for scan %s (%s); skipping\n", scanNumber, formKey)
				continue
			}
			count, err := copyResource(resource, filepath.Join(stagingDir, destination), destination)
			if err != nil {
				fatal(err)
			}
			copied += count
		}
		if copied == 0 {
			fmt.Fprintf(os.Stderr, "warning: scan-map-%s had no mapped NIFTI files; no zip created\n", bundleName)
			os.RemoveAll(stagingDir)
			continue
		}
		zipPath := filepath.Join(outputDir, safeSessionLabel+"_"+bundleName+".zip")
		if err := zipDirectory(stagingDir, zipPath); err != nil {
			fatal(err)
		}
		os.RemoveAll(stagingDir)
		fmt.Printf("Wrote %s with %d file(s).\n", zipPath, copied)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
