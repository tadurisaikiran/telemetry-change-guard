# AWS synthesized input boundary

Telemetry Change Guard has a production-bounded foundation for reading AWS
infrastructure as data. The `adapters/cloudformation` package accepts either a
standalone synthesized CloudFormation JSON template or a Cloud Assembly
directory (including a direct path to its `manifest.json`). It does not parse
or execute CDK source code.

This is the first implementation phase of the accepted
[AWS identity architecture](adr/0005-aws-cloudwatch-identity-and-migration-semantics.md).
It establishes deterministic input and provenance before later phases resolve
intrinsics and discover CloudWatch consumers.

## Accepted inputs

- A standalone synthesized CloudFormation **JSON** template file.
- A Cloud Assembly directory containing `manifest.json`.
- A direct path to a Cloud Assembly `manifest.json`.
- Nested `cdk:cloud-assembly` artifacts within the selected assembly root.
- Multiple `aws:cloudformation:stack` artifacts, returned in stable qualified
  artifact-ID order.

YAML templates, CDK application source, cloud-hosted templates, and live AWS
API resources are outside this loader. A CDK application must first be
synthesized by the caller in its own trusted build step.

## Security and validation

The loader treats every byte in a template or assembly as untrusted input:

- JSON must be valid UTF-8 and contain exactly one value. Duplicate object
  keys at any depth are rejected instead of silently selecting one value.
- The template root and declared sections are type checked. `Resources` is
  required, logical IDs are validated, resource types are required, and
  resources are sorted deterministically.
- Cloud Assembly semantic versions must have a supported schema major. The
  current maximum is exposed as `cloudformation.MaxSupportedAssemblyMajor`.
  A newer major fails closed instead of being interpreted as an older schema.
- A non-empty Cloud Assembly `missing` list is rejected because its synthesis
  context is incomplete.
- Artifact dependencies must exist and be acyclic. Unknown artifact types are
  rejected; known non-stack artifacts are ignored without opening or running
  their commands.
- Manifest paths must be clean, relative slash paths. Rooted file access also
  blocks traversal and symbolic-link escape from the selected assembly.
- Transforms and intrinsic functions are preserved as raw JSON but are never
  executed or guessed.
- Context cancellation is checked around local I/O and deterministic traversal.

Default limits are:

| Boundary | Default |
| --- | ---: |
| One synthesized template | 1 MiB |
| One assembly manifest | 8 MiB |
| Templates across an assembly | 64 MiB |
| Manifests across an assembly | 64 MiB |
| Resources in one template | 500 |
| Stack artifacts | 512 |
| All artifacts | 4,096 |
| Nested assembly depth | 16 |
| JSON nesting depth | 128 |
| JSON tokens per file | 1,000,000 |

Zero-valued `cloudformation.Limits` fields use these defaults; negative limits
are rejected. The 1 MiB template and 500-resource boundaries align with
CloudFormation template quotas. Aggregate limits additionally protect local
and CI analysis of multi-stack assemblies.

## Determinism and provenance

Every stack includes its local artifact ID, collision-safe qualified ID,
assembly path, declared environment, sorted dependencies, manifest file, and
template file. Every resource includes the same origin plus its logical ID and
optional stack name. CloudFormation maps needed by later phases are exposed as
sorted name/value slices; unresolved values remain exact `json.RawMessage`
data.

The stack environment is accepted only in the CDK
`aws://account/region` form. Known accounts must be 12 digits. Assemblies that
are environment agnostic retain `unknown-account` and `unknown-region`; the loader
does not replace them with the machine's credentials or Region.

## Intentional analysis boundary

The loader is a programmatic foundation in this phase, not yet a CLI or
configuration source. Wiring raw templates into the safety engine before it
understands CloudWatch alarms could incorrectly report `PASS` when consumers
are present. Analysis integration will occur after the supported intrinsic
subset has explicit `EXACT`/`PARTIAL`/`UNKNOWN` outcomes and standard alarms
fail closed on unresolved identity fields.

## Primary references

- [AWS CloudFormation quotas](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/cloudformation-limits.html)
- [AWS CDK synthesis](https://docs.aws.amazon.com/cdk/v2/guide/ref-cli-cmd-synth.html)
- [AWS Cloud Assembly schema](https://github.com/aws/aws-cdk-cli/tree/main/packages/%40aws-cdk/cloud-assembly-schema/schema)
