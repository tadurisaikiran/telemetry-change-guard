#!/usr/bin/env bash
set -euo pipefail

repository_root="${TCG_REPOSITORY_ROOT:-${GITHUB_WORKSPACE:-${PWD}}}"

: "${RUNNER_TEMP:?RUNNER_TEMP is required}"
: "${GITHUB_STEP_SUMMARY:?GITHUB_STEP_SUMMARY is required}"
: "${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"

report_path="${RUNNER_TEMP}/telemetry-change-guard-report.md"
json_report_path="${RUNNER_TEMP}/telemetry-change-guard-result.json"
status_output_path="${RUNNER_TEMP}/telemetry-change-guard-status"

write_error_artifacts() {
  local message=$1
  printf '# Telemetry Change Guard\n\n**Status:** **ERROR**\n\n%s\n' "${message}" > "${report_path}"
  printf '{\n  "schemaVersion": "tcg-action-error/v1alpha1",\n  "status": "ERROR",\n  "errors": ["%s"]\n}\n' \
    "${message}" > "${json_report_path}"
  printf 'ERROR\n' > "${status_output_path}"
}

mode="invalid"
exit_code=1
generic_source_count=0
generic_source=""
configuration_error=""
case "${TCG_REMOTE_EVIDENCE:-disabled}" in
  disabled)
    if [[ -n "${TCG_ALLOWED_REMOTE_ORIGINS:-}" || "${TCG_ALLOW_INSECURE_LOOPBACK:-false}" != "false" || -n "${TCG_REMOTE_BEARER_TOKEN:-}" ]]; then
      configuration_error="Configuration error: allowed-remote-origins, allow-insecure-loopback, and remote-bearer-token require remote-evidence enabled."
    fi
    ;;
  enabled)
    if [[ -z "${TCG_ALLOWED_REMOTE_ORIGINS:-}" ]]; then
      configuration_error="Configuration error: remote-evidence enabled requires at least one trusted allowed-remote-origin."
    fi
    ;;
  *)
    configuration_error="Configuration error: remote-evidence must be disabled or enabled."
    ;;
esac
if [[ "${TCG_ALLOW_INSECURE_LOOPBACK:-false}" != "false" && "${TCG_ALLOW_INSECURE_LOOPBACK:-false}" != "true" ]]; then
  configuration_error="Configuration error: allow-insecure-loopback must be true or false."
fi
if [[ -n "${TCG_CHANGES:-}" ]]; then
  generic_source_count=$((generic_source_count + 1))
  generic_source="changes"
fi
if [[ -n "${TCG_BASELINE:-}" || -n "${TCG_CANDIDATE:-}" ]]; then
  if [[ -z "${TCG_BASELINE:-}" || -z "${TCG_CANDIDATE:-}" ]]; then
    configuration_error="Configuration error: baseline and candidate must be provided together."
  else
    generic_source_count=$((generic_source_count + 1))
    generic_source="snapshot"
  fi
fi
if [[ -n "${TCG_WEAVER_DIFF:-}" || -n "${TCG_WEAVER_MAPPING:-}" ]]; then
  if [[ -z "${TCG_WEAVER_DIFF:-}" || -z "${TCG_WEAVER_MAPPING:-}" ]]; then
    configuration_error="Configuration error: weaver-diff and weaver-mapping must be provided together."
  else
    generic_source_count=$((generic_source_count + 1))
    generic_source="weaver"
  fi
fi

if [[ -n "${configuration_error}" ]]; then
  write_error_artifacts "${configuration_error}"
elif [[ -n "${TCG_MIGRATION:-}" && "${generic_source_count}" -ne 0 ]]; then
  write_error_artifacts "Configuration error: generic change sources and migration are mutually exclusive."
elif [[ -z "${TCG_MIGRATION:-}" && "${generic_source_count}" -ne 1 ]]; then
  write_error_artifacts "Configuration error: exactly one generic change source or migration is required."
else
  command=("${RUNNER_TEMP}/telemetry-change-guard")
  if [[ "${generic_source_count}" -eq 1 ]]; then
    mode="generic"
    command+=(check --config "${TCG_CONFIG}" --repository-root "${repository_root}" --remote-evidence "${TCG_REMOTE_EVIDENCE:-disabled}")
    case "${generic_source}" in
      changes) command+=(--changes "${TCG_CHANGES}") ;;
      snapshot) command+=(--baseline "${TCG_BASELINE}" --candidate "${TCG_CANDIDATE}") ;;
      weaver) command+=(--weaver-diff "${TCG_WEAVER_DIFF}" --weaver-mapping "${TCG_WEAVER_MAPPING}") ;;
      *)
        write_error_artifacts "Configuration error: internal generic change-source selection failed."
        command=()
        ;;
    esac
  else
    mode="migration"
    command+=(migration check --config "${TCG_CONFIG}" --plan "${TCG_MIGRATION}" --repository-root "${repository_root}" --remote-evidence "${TCG_REMOTE_EVIDENCE:-disabled}")
  fi

  if [[ "${TCG_REMOTE_EVIDENCE:-disabled}" == "enabled" ]]; then
    while IFS= read -r origin; do
      [[ -z "${origin}" ]] && continue
      command+=(--allowed-remote-origin "${origin}")
    done <<< "${TCG_ALLOWED_REMOTE_ORIGINS}"
    if [[ "${TCG_ALLOW_INSECURE_LOOPBACK:-false}" == "true" ]]; then
      command+=(--allow-insecure-loopback)
    fi
  fi

  if [[ "${#command[@]}" -ne 0 ]]; then
    set +e
    analysis_environment=(env -i)
    if [[ "${TCG_REMOTE_EVIDENCE:-disabled}" == "enabled" && -n "${TCG_REMOTE_BEARER_TOKEN:-}" ]]; then
      analysis_environment+=(TCG_REMOTE_BEARER_TOKEN="${TCG_REMOTE_BEARER_TOKEN}")
    fi
    "${analysis_environment[@]}" "${command[@]}" \
      --format markdown \
      --output "${report_path}" \
      --json-output "${json_report_path}" \
      --status-output "${status_output_path}"
    exit_code=$?
    set -e
  fi

  if [[ ! -s "${report_path}" || ! -s "${json_report_path}" || ! -s "${status_output_path}" ]]; then
    exit_code=1
    write_error_artifacts "Analysis failed before complete report artifacts were produced. See the step log for details."
  fi
fi

status=$(<"${status_output_path}")
schema_version=$(sed -nE 's/^  "schemaVersion": "([^"]+)",?$/\1/p' "${json_report_path}")
case "${mode}:${schema_version}:${status}" in
  generic:tcg-result/v1alpha1:PASS | generic:tcg-result/v1alpha1:WARN | generic:tcg-result/v1alpha1:BLOCK | generic:tcg-result/v1alpha1:INCOMPLETE | generic:tcg-result/v1alpha1:ERROR) ;;
  migration:tmr-result/v1alpha1:READY | migration:tmr-result/v1alpha1:BLOCKED | migration:tmr-result/v1alpha1:INCOMPLETE | migration:tmr-result/v1alpha1:ERROR) ;;
  invalid:tcg-action-error/v1alpha1:ERROR) ;;
  *)
    status="ERROR"
    exit_code=1
    write_error_artifacts "Analysis produced an unrecognized schema or mode-incompatible status. The Action failed closed."
    ;;
esac

expected_exit=1
case "${status}" in
  PASS|WARN|READY) expected_exit=0 ;;
  BLOCK|BLOCKED) expected_exit=2 ;;
  INCOMPLETE) expected_exit=3 ;;
  ERROR) expected_exit=1 ;;
  *) expected_exit=1 ;;
esac

if [[ "${exit_code}" -ne "${expected_exit}" ]]; then
  status="ERROR"
  exit_code=1
  write_error_artifacts "Analysis status and process exit code disagreed. The Action failed closed."
fi

cat "${report_path}" >> "${GITHUB_STEP_SUMMARY}"
{
  printf 'status=%s\n' "${status}"
  printf 'exit-code=%s\n' "${exit_code}"
  printf 'report=%s\n' "${report_path}"
  printf 'json-report=%s\n' "${json_report_path}"
  printf 'mode=%s\n' "${mode}"
} >> "${GITHUB_OUTPUT}"

exit 0
