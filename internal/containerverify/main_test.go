package main

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var testIdentity = expectedIdentity{
	Version:  "0.1.0-alpha.1",
	Revision: strings.Repeat("a", 40),
	Created:  "2026-08-25T12:00:00Z",
}

func TestVerifyLayoutAcceptsTwoPlatformsWithBoundAttestations(t *testing.T) {
	t.Parallel()

	file := writeTestLayout(t, testLayoutOptions{})
	if err := verifyLayout(file, testIdentity); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyLayoutAcceptsBuildKitNestedImageIndex(t *testing.T) {
	t.Parallel()

	file := writeTestLayout(t, testLayoutOptions{nestedIndex: true})
	if err := verifyLayout(file, testIdentity); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyLayoutAcceptsBuildKitAttestationManifest(t *testing.T) {
	t.Parallel()

	file := writeTestLayout(t, testLayoutOptions{buildKitAttestation: true})
	if err := verifyLayout(file, testIdentity); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyLayoutRejectsRootRuntime(t *testing.T) {
	t.Parallel()

	file := writeTestLayout(t, testLayoutOptions{runtimeUser: "root"})
	if err := verifyLayout(file, testIdentity); err == nil || !strings.Contains(err.Error(), "runtime user") {
		t.Fatalf("error = %v; want unsafe runtime user", err)
	}
}

func TestVerifyLayoutRejectsMissingSLSAProvenance(t *testing.T) {
	t.Parallel()

	file := writeTestLayout(t, testLayoutOptions{omitSLSA: true})
	if err := verifyLayout(file, testIdentity); err == nil || !strings.Contains(err.Error(), "SLSA provenance") {
		t.Fatalf("error = %v; want missing SLSA provenance", err)
	}
}

func TestVerifyLayoutRejectsMismatchedOCIAttestationSubject(t *testing.T) {
	t.Parallel()

	file := writeTestLayout(t, testLayoutOptions{mismatchedManifestSubject: true})
	if err := verifyLayout(file, testIdentity); err == nil || !strings.Contains(err.Error(), "does not bind") {
		t.Fatalf("error = %v; want mismatched OCI attestation subject", err)
	}
}

func TestVerifyLayoutRejectsMismatchedBuildKitAttestationConfig(t *testing.T) {
	t.Parallel()

	file := writeTestLayout(t, testLayoutOptions{buildKitAttestation: true, mismatchedConfigLayer: true})
	if err := verifyLayout(file, testIdentity); err == nil || !strings.Contains(err.Error(), "does not match its statement") {
		t.Fatalf("error = %v; want mismatched BuildKit attestation layer", err)
	}
}

func TestVerifyLayoutRejectsUnsupportedInTotoStatementVersion(t *testing.T) {
	t.Parallel()

	file := writeTestLayout(t, testLayoutOptions{buildKitAttestation: true, invalidStatementType: true})
	if err := verifyLayout(file, testIdentity); err == nil || !strings.Contains(err.Error(), "invalid in-toto statement type") {
		t.Fatalf("error = %v; want invalid in-toto statement type", err)
	}
}

type testLayoutOptions struct {
	runtimeUser               string
	omitSLSA                  bool
	nestedIndex               bool
	mismatchedManifestSubject bool
	buildKitAttestation       bool
	mismatchedConfigLayer     bool
	invalidStatementType      bool
}

type testLayoutBuilder struct {
	t     *testing.T
	files map[string][]byte
}

func writeTestLayout(t *testing.T, options testLayoutOptions) string {
	t.Helper()
	if options.runtimeUser == "" {
		options.runtimeUser = expectedRuntimeUser
	}
	builder := &testLayoutBuilder{t: t, files: map[string][]byte{
		"oci-layout": []byte("{\"imageLayoutVersion\":\"1.0.0\"}\n"),
	}}
	descriptors := []descriptor{}
	for _, architecture := range []string{"amd64", "arm64"} {
		runtime := builder.addRuntime(architecture, options.runtimeUser)
		descriptors = append(descriptors, runtime)
		descriptors = append(descriptors, builder.addAttestation(runtime, options))
	}
	index := imageIndex{SchemaVersion: 2, MediaType: indexMediaType, Manifests: descriptors}
	if options.nestedIndex {
		index = imageIndex{
			SchemaVersion: 2,
			MediaType:     indexMediaType,
			Manifests:     []descriptor{builder.addBlob(marshalTestJSON(t, index), indexMediaType)},
		}
	}
	builder.files["index.json"] = marshalTestJSON(t, index)

	file := filepath.Join(t.TempDir(), "image.oci.tar")
	output, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(output)
	names := make([]string, 0, len(builder.files))
	for name := range builder.files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := builder.files[name]
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	return file
}

func (builder *testLayoutBuilder) addRuntime(architecture, user string) descriptor {
	config := imageConfig{Architecture: architecture, OS: "linux"}
	config.Config.User = user
	config.Config.Entrypoint = []string{expectedEntrypoint}
	config.Config.WorkingDir = expectedWorkingDir
	config.Config.Labels = map[string]string{
		"org.opencontainers.image.title":         "Telemetry Change Guard",
		"org.opencontainers.image.description":   expectedDescription,
		"org.opencontainers.image.url":           expectedSource,
		"org.opencontainers.image.source":        expectedSource,
		"org.opencontainers.image.documentation": expectedDocumentation,
		"org.opencontainers.image.licenses":      expectedLicense,
		"org.opencontainers.image.version":       testIdentity.Version,
		"org.opencontainers.image.revision":      testIdentity.Revision,
		"org.opencontainers.image.created":       testIdentity.Created,
	}
	configDescriptor := builder.addBlob(marshalTestJSON(builder.t, config), "application/vnd.oci.image.config.v1+json")
	layerDescriptor := builder.addBlob([]byte("runtime-layer-"+architecture), "application/vnd.oci.image.layer.v1.tar+gzip")
	manifest := imageManifest{
		SchemaVersion: 2,
		MediaType:     manifestMediaType,
		Config:        configDescriptor,
		Layers:        []descriptor{layerDescriptor},
	}
	descriptor := builder.addBlob(marshalTestJSON(builder.t, manifest), manifestMediaType)
	descriptor.Platform = &platform{Architecture: architecture, OS: "linux"}
	return descriptor
}

func (builder *testLayoutBuilder) addAttestation(runtime descriptor, options testLayoutOptions) descriptor {
	layers := []descriptor{
		builder.addStatement(runtime.Digest, "https://spdx.dev/Document", options),
	}
	if !options.omitSLSA {
		layers = append(layers, builder.addStatement(runtime.Digest, "https://slsa.dev/provenance/v1", options))
	}
	if options.buildKitAttestation {
		diffIDs := make([]string, len(layers))
		for index, layer := range layers {
			diffIDs[index] = layer.Digest
		}
		if options.mismatchedConfigLayer {
			diffIDs[0] = "sha256:" + strings.Repeat("c", 64)
		}
		configDocument := buildKitAttestationConfig{Architecture: "unknown", OS: "unknown", Config: map[string]json.RawMessage{}}
		configDocument.RootFS.Type = "layers"
		configDocument.RootFS.DiffIDs = diffIDs
		config := builder.addBlob(marshalTestJSON(builder.t, configDocument), imageConfigMediaType)
		manifest := imageManifest{
			SchemaVersion: 2,
			MediaType:     manifestMediaType,
			Config:        config,
			Layers:        layers,
		}
		descriptor := builder.addBlob(marshalTestJSON(builder.t, manifest), manifestMediaType)
		descriptor.Platform = &platform{Architecture: "unknown", OS: "unknown"}
		descriptor.Annotations = map[string]string{
			referenceTypeKey:   attestationType,
			referenceDigestKey: runtime.Digest,
		}
		return descriptor
	}
	config := builder.addBlob([]byte("{}"), emptyConfigMediaType)
	subject := runtime
	if options.mismatchedManifestSubject {
		subject.Digest = "sha256:" + strings.Repeat("b", 64)
	}
	manifest := imageManifest{
		SchemaVersion: 2,
		MediaType:     manifestMediaType,
		ArtifactType:  attestationMediaType,
		Config:        config,
		Layers:        layers,
		Subject:       &subject,
	}
	descriptor := builder.addBlob(marshalTestJSON(builder.t, manifest), manifestMediaType)
	descriptor.Platform = &platform{Architecture: "unknown", OS: "unknown"}
	descriptor.Annotations = map[string]string{
		referenceTypeKey:   attestationType,
		referenceDigestKey: runtime.Digest,
	}
	return descriptor
}

func (builder *testLayoutBuilder) addStatement(subjectDigest, predicateType string, options testLayoutOptions) descriptor {
	statementType := inTotoStatementV1
	if options.buildKitAttestation {
		statementType = inTotoStatementV01
	}
	if options.invalidStatementType {
		statementType = "https://in-toto.io/Statement/unsupported"
	}
	document := map[string]any{
		"_type":         statementType,
		"predicateType": predicateType,
		"subject": []any{map[string]any{
			"name": "telemetry-change-guard",
			"digest": map[string]string{
				"sha256": strings.TrimPrefix(subjectDigest, "sha256:"),
			},
		}},
		"predicate": map[string]any{},
	}
	descriptor := builder.addBlob(marshalTestJSON(builder.t, document), inTotoMediaType)
	descriptor.Annotations = map[string]string{predicateTypeKey: predicateType}
	return descriptor
}

func (builder *testLayoutBuilder) addBlob(data []byte, mediaType string) descriptor {
	digest := sha256.Sum256(data)
	hexDigest := hex.EncodeToString(digest[:])
	builder.files["blobs/sha256/"+hexDigest] = data
	return descriptor{MediaType: mediaType, Digest: "sha256:" + hexDigest, Size: int64(len(data))}
}

func marshalTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
