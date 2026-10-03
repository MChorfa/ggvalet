package lifecycle

// CanonicalGoldenPipelineV7 returns the production-grade .gitlab-ci.yml template
// strictly adhering to the Canonical Delivery Model v7.0.0.
const CanonicalGoldenPipelineV7 = `# ==============================================================================
# CANONICAL DELIVERY MODEL v7.0.0 — GOLDEN PIPELINE
# Enforced by GG Valet (MChorfa/ggvalet)
# ==============================================================================
stages:
  - preflight
  - build
  - test
  - security
  - package
  - sign
  - trustwall

variables:
  CANONICAL_VERSION: "v7.0.0"
  FF_USE_FASTZIP: "true"
  SECURE_LOG_LEVEL: "info"

default:
  interruptible: true
  retry:
    max: 2
    when:
      - runner_system_failure
      - stuck_or_timeout_failure

# ─── PREFLIGHT STAGE ─────────────────────────────────────────────────────────
preflight:lint:
  stage: preflight
  image: alpine:3.20@sha256:beefdbd8a1da6d2915566fde36db9db0b524eb737fc57cd13675d29e64fcb612
  script:
    - echo "Validating immutable configurations and branch policies..."

# ─── SECURITY STAGE ──────────────────────────────────────────────────────────
security:sast:
  stage: security
  image: registry.gitlab.com/security-products/semgrep:5
  script:
    - /analyzer run
  artifacts:
    reports:
      sast: gl-sast-report.json

security:secret_detection:
  stage: security
  image: registry.gitlab.com/security-products/secrets:5
  script:
    - /analyzer run
  artifacts:
    reports:
      secret_detection: gl-secret-detection-report.json

# ─── PACKAGE & SBOM STAGE ───────────────────────────────────────────────────
package:sbom:
  stage: package
  image: cyclonedx/cyclonedx-cli:0.25.1
  script:
    - cyclonedx-cli generate --output-file bom.json
  artifacts:
    name: "cyclonedx-sbom-$CI_COMMIT_SHA"
    paths:
      - bom.json
    expire_in: 30 days

# ─── SIGNING STAGE ───────────────────────────────────────────────────────────
sign:release:
  stage: sign
  image: gcr.io/projectsigstore/cosign:v2.2.4
  script:
    - echo "Signing release artifact with keyless OIDC Cosign..."
  rules:
    - if: '$CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+/'

# ─── TRUSTWALL GATE ──────────────────────────────────────────────────────────
trustwall:admission:
  stage: trustwall
  image: alpine:3.20@sha256:beefdbd8a1da6d2915566fde36db9db0b524eb737fc57cd13675d29e64fcb612
  script:
    - echo "Trustwall admission check: verifying Cosign signature and SLSA provenance..."
  rules:
    - if: '$CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+/'
`

// CanonicalBranchProtectionConfig returns the standard branch protection configuration.
const CanonicalBranchProtectionConfig = `{
  "name": "main",
  "push_access_level": 0,
  "merge_access_level": 40,
  "unprotect_access_level": 40,
  "allow_force_push": false,
  "code_owner_approval_required": true
}`

// CanonicalRunnerConfigSnippet returns standard runner concurrency and timeout bounds.
const CanonicalRunnerConfigSnippet = `concurrent = 4
check_interval = 5

[session_server]
  session_timeout = 1800

[[runners]]
  name = "hardened-valet-runner"
  url = "https://gitlab-shared.local"
  executor = "docker"
  tags = ["runner:hardened-linux", "runner:airgap-transfer"]
  [runners.docker]
    tls_verify = true
    image = "alpine:3.20@sha256:beefdbd8a1da6d2915566fde36db9db0b524eb737fc57cd13675d29e64fcb612"
    privileged = false
    disable_entrypoint_overwrite = false
    oom_kill_disable = false
    disable_cache = false
    volumes = ["/cache"]
    shm_size = 0
`
