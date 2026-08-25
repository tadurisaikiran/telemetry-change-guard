// Command containerverify verifies the structure and attestations of the
// unpublished multi-platform OCI layout produced by the release pipeline.
package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

const (
	indexMediaType        = "application/vnd.oci.image.index.v1+json"
	manifestMediaType     = "application/vnd.oci.image.manifest.v1+json"
	attestationType       = "attestation-manifest"
	referenceDigestKey    = "vnd.docker.reference.digest"
	referenceTypeKey      = "vnd.docker.reference.type"
	maxLayoutEntryBytes   = 512 << 20
	maxLayoutTotalBytes   = 2 << 30
	expectedEntrypoint    = "/telemetry-change-guard"
	expectedWorkingDir    = "/workspace"
	expectedRuntimeUser   = "nonroot:nonroot"
	expectedSource        = "https://github.com/tadurisaikiran/telemetry-change-guard"
	expectedLicense       = "Apache-2.0"
	expectedDescription   = "Deterministic telemetry contract change-impact analysis"
	expectedDocumentation = "https://github.com/tadurisaikiran/telemetry-change-guard/blob/main/docs/INSTALLATION.md"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type descriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Platform    *platform         `json:"platform,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type platform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
}

type imageIndex struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	Manifests     []descriptor `json:"manifests"`
}

type imageManifest struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	Config        descriptor   `json:"config"`
	Layers        []descriptor `json:"layers"`
}

type imageConfig struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Config       struct {
		User       string            `json:"User"`
		Entrypoint []string          `json:"Entrypoint"`
		WorkingDir string            `json:"WorkingDir"`
		Labels     map[string]string `json:"Labels"`
	} `json:"config"`
}

type statement struct {
	PredicateType string `json:"predicateType"`
	Subject       []struct {
		Digest map[string]string `json:"digest"`
	} `json:"subject"`
}

type expectedIdentity struct {
	Version  string
	Revision string
	Created  string
}

type layout struct {
	files map[string][]byte
}

func main() {
	layoutPath := flag.String("layout", "", "path to an OCI layout tar archive")
	version := flag.String("version", "", "expected OCI version label")
	revision := flag.String("revision", "", "expected full source commit")
	created := flag.String("created", "", "expected RFC3339 build date label")
	flag.Parse()
	if flag.NArg() != 0 || *layoutPath == "" || *version == "" || *created == "" || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(*revision) {
		fmt.Fprintln(os.Stderr, "usage: containerverify --layout <oci.tar> --version <version> --revision <40-char-sha> --created <RFC3339>")
		os.Exit(2)
	}
	if err := verifyLayout(*layoutPath, expectedIdentity{Version: *version, Revision: *revision, Created: *created}); err != nil {
		fmt.Fprintf(os.Stderr, "container verification failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Verified linux/amd64 and linux/arm64 OCI images with per-platform SPDX SBOM and SLSA provenance")
}

func verifyLayout(file string, expected expectedIdentity) error {
	contents, err := readLayout(file)
	if err != nil {
		return err
	}
	if string(contents.files["oci-layout"]) != "{\"imageLayoutVersion\":\"1.0.0\"}\n" &&
		string(contents.files["oci-layout"]) != "{\"imageLayoutVersion\": \"1.0.0\"}\n" {
		var document struct {
			Version string `json:"imageLayoutVersion"`
		}
		if err := json.Unmarshal(contents.files["oci-layout"], &document); err != nil || document.Version != "1.0.0" {
			return errors.New("oci-layout does not declare version 1.0.0")
		}
	}
	var index imageIndex
	if err := decodeJSON(contents.files["index.json"], &index); err != nil {
		return fmt.Errorf("decode index.json: %w", err)
	}
	manifests, err := contents.collectManifests(index, map[string]struct{}{}, 0)
	if err != nil {
		return fmt.Errorf("walk OCI image indexes: %w", err)
	}

	runtimeDigests := map[string]string{}
	attestations := map[string][]descriptor{}
	for _, item := range manifests {
		if item.Annotations[referenceTypeKey] == attestationType {
			reference := item.Annotations[referenceDigestKey]
			if !digestPattern.MatchString(reference) {
				return fmt.Errorf("attestation %s has invalid reference digest %q", item.Digest, reference)
			}
			attestations[reference] = append(attestations[reference], item)
			continue
		}
		if item.Platform == nil || item.Platform.OS != "linux" || (item.Platform.Architecture != "amd64" && item.Platform.Architecture != "arm64") {
			return fmt.Errorf("unexpected runtime platform on %s", item.Digest)
		}
		key := item.Platform.OS + "/" + item.Platform.Architecture
		if _, duplicate := runtimeDigests[key]; duplicate {
			return fmt.Errorf("duplicate runtime platform %s", key)
		}
		if err := contents.verifyRuntime(item, expected); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		runtimeDigests[key] = item.Digest
	}
	for _, required := range []string{"linux/amd64", "linux/arm64"} {
		digest := runtimeDigests[required]
		if digest == "" {
			return fmt.Errorf("missing runtime platform %s", required)
		}
		if err := contents.verifyAttestations(digest, attestations[digest]); err != nil {
			return fmt.Errorf("%s attestations: %w", required, err)
		}
	}
	if len(runtimeDigests) != 2 {
		return fmt.Errorf("found %d runtime platforms; expected exactly 2", len(runtimeDigests))
	}
	for reference := range attestations {
		found := false
		for _, digest := range runtimeDigests {
			found = found || reference == digest
		}
		if !found {
			return fmt.Errorf("attestation references unknown manifest %s", reference)
		}
	}
	return nil
}

func (l layout) collectManifests(index imageIndex, visited map[string]struct{}, depth int) ([]descriptor, error) {
	const maxIndexDepth = 8
	if depth > maxIndexDepth {
		return nil, fmt.Errorf("OCI image index nesting exceeds %d levels", maxIndexDepth)
	}
	if index.SchemaVersion != 2 || index.MediaType != indexMediaType || len(index.Manifests) == 0 {
		return nil, errors.New("encountered an invalid or empty OCI image index")
	}

	var manifests []descriptor
	for _, item := range index.Manifests {
		switch item.MediaType {
		case manifestMediaType:
			manifests = append(manifests, item)
		case indexMediaType:
			if _, duplicate := visited[item.Digest]; duplicate {
				return nil, fmt.Errorf("duplicate or cyclic nested image index %s", item.Digest)
			}
			visited[item.Digest] = struct{}{}
			rawIndex, err := l.blob(item)
			if err != nil {
				return nil, err
			}
			var nested imageIndex
			if err := decodeJSON(rawIndex, &nested); err != nil {
				return nil, fmt.Errorf("decode nested image index %s: %w", item.Digest, err)
			}
			children, err := l.collectManifests(nested, visited, depth+1)
			if err != nil {
				return nil, err
			}
			manifests = append(manifests, children...)
		default:
			return nil, fmt.Errorf("unsupported index descriptor media type %q", item.MediaType)
		}
	}
	return manifests, nil
}

func readLayout(file string) (layout, error) {
	input, err := os.Open(file)
	if err != nil {
		return layout{}, err
	}
	defer input.Close()
	reader := tar.NewReader(input)
	files := map[string][]byte{}
	var total int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return layout{}, err
		}
		name := strings.TrimSuffix(header.Name, "/")
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || path.Clean(name) != name || strings.HasPrefix(name, "../") {
			return layout{}, fmt.Errorf("unsafe layout path %q", header.Name)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return layout{}, fmt.Errorf("non-regular layout entry %q", header.Name)
		}
		if header.Size < 0 || header.Size > maxLayoutEntryBytes || total+header.Size > maxLayoutTotalBytes {
			return layout{}, fmt.Errorf("layout entry %q exceeds size limits", header.Name)
		}
		if _, duplicate := files[name]; duplicate {
			return layout{}, fmt.Errorf("duplicate layout entry %q", header.Name)
		}
		data, err := io.ReadAll(io.LimitReader(reader, maxLayoutEntryBytes+1))
		if err != nil {
			return layout{}, err
		}
		if int64(len(data)) != header.Size {
			return layout{}, fmt.Errorf("truncated layout entry %q", header.Name)
		}
		files[name] = data
		total += header.Size
	}
	if files["oci-layout"] == nil || files["index.json"] == nil {
		return layout{}, errors.New("layout is missing oci-layout or index.json")
	}
	return layout{files: files}, nil
}

func (l layout) blob(item descriptor) ([]byte, error) {
	if !digestPattern.MatchString(item.Digest) || item.Size < 0 {
		return nil, fmt.Errorf("invalid descriptor digest or size for %q", item.Digest)
	}
	hexDigest := strings.TrimPrefix(item.Digest, "sha256:")
	data, ok := l.files["blobs/sha256/"+hexDigest]
	if !ok {
		return nil, fmt.Errorf("missing blob %s", item.Digest)
	}
	if int64(len(data)) != item.Size {
		return nil, fmt.Errorf("blob %s size mismatch", item.Digest)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != hexDigest {
		return nil, fmt.Errorf("blob %s digest mismatch", item.Digest)
	}
	return data, nil
}

func (l layout) verifyRuntime(item descriptor, expected expectedIdentity) error {
	if item.MediaType != manifestMediaType {
		return fmt.Errorf("runtime descriptor has media type %q", item.MediaType)
	}
	rawManifest, err := l.blob(item)
	if err != nil {
		return err
	}
	var manifest imageManifest
	if err := decodeJSON(rawManifest, &manifest); err != nil {
		return err
	}
	if manifest.SchemaVersion != 2 || manifest.MediaType != manifestMediaType || len(manifest.Layers) == 0 {
		return errors.New("invalid runtime image manifest")
	}
	rawConfig, err := l.blob(manifest.Config)
	if err != nil {
		return err
	}
	var config imageConfig
	if err := decodeJSON(rawConfig, &config); err != nil {
		return err
	}
	if config.OS != item.Platform.OS || config.Architecture != item.Platform.Architecture {
		return errors.New("config platform differs from manifest platform")
	}
	if config.Config.User != expectedRuntimeUser || !equalStrings(config.Config.Entrypoint, []string{expectedEntrypoint}) || config.Config.WorkingDir != expectedWorkingDir {
		return errors.New("runtime user, entrypoint, or working directory is unsafe")
	}
	expectedLabels := map[string]string{
		"org.opencontainers.image.title":         "Telemetry Change Guard",
		"org.opencontainers.image.description":   expectedDescription,
		"org.opencontainers.image.url":           expectedSource,
		"org.opencontainers.image.source":        expectedSource,
		"org.opencontainers.image.documentation": expectedDocumentation,
		"org.opencontainers.image.licenses":      expectedLicense,
		"org.opencontainers.image.version":       expected.Version,
		"org.opencontainers.image.revision":      expected.Revision,
		"org.opencontainers.image.created":       expected.Created,
	}
	for key, value := range expectedLabels {
		if config.Config.Labels[key] != value {
			return fmt.Errorf("label %s=%q; expected %q", key, config.Config.Labels[key], value)
		}
	}
	for _, layer := range manifest.Layers {
		if _, err := l.blob(layer); err != nil {
			return err
		}
	}
	return nil
}

func (l layout) verifyAttestations(subjectDigest string, items []descriptor) error {
	if len(items) == 0 {
		return errors.New("missing attestation manifest")
	}
	wantSubject := strings.TrimPrefix(subjectDigest, "sha256:")
	foundSPDX := false
	foundSLSA := false
	for _, item := range items {
		if item.MediaType != manifestMediaType {
			return fmt.Errorf("attestation descriptor has media type %q", item.MediaType)
		}
		rawManifest, err := l.blob(item)
		if err != nil {
			return err
		}
		var manifest imageManifest
		if err := decodeJSON(rawManifest, &manifest); err != nil {
			return err
		}
		if _, err := l.blob(manifest.Config); err != nil {
			return err
		}
		for _, layer := range manifest.Layers {
			rawStatement, err := l.blob(layer)
			if err != nil {
				return err
			}
			var document statement
			if err := decodeJSON(rawStatement, &document); err != nil {
				return fmt.Errorf("decode in-toto statement: %w", err)
			}
			subjectMatches := false
			for _, subject := range document.Subject {
				subjectMatches = subjectMatches || subject.Digest["sha256"] == wantSubject
			}
			if !subjectMatches {
				return fmt.Errorf("attestation predicate %q does not bind %s", document.PredicateType, subjectDigest)
			}
			predicate := strings.ToLower(document.PredicateType)
			foundSPDX = foundSPDX || strings.Contains(predicate, "spdx")
			foundSLSA = foundSLSA || strings.Contains(predicate, "slsa")
		}
	}
	if !foundSPDX || !foundSLSA {
		missing := []string{}
		if !foundSPDX {
			missing = append(missing, "SPDX SBOM")
		}
		if !foundSLSA {
			missing = append(missing, "SLSA provenance")
		}
		sort.Strings(missing)
		return fmt.Errorf("missing %s", strings.Join(missing, " and "))
	}
	return nil
}

func decodeJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON contains multiple values")
		}
		return err
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
