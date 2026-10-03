# How-To: Verify Claims and Transmit Bundles Across Hardware Diodes

This guide explains how to package software artifacts, verify mandatory security invariants at the Trustwall gate, and transmit immutable evidence bundles across unidirectional hardware diodes into air-gapped environments.

---

## 1. Verifying Artifact Claims at the Trustwall Gate

Before any binary or container image is admitted for promotion, its cryptographic claims must be evaluated:

```bash
ggvalet trustwall verify \
  --artifact-id "core-runtime" \
  --digest "sha256:1111222233334444" \
  --source "git@gitlab:org/repo.git" \
  --commit "a1b2c3d4e5" \
  --signed \
  --sbom \
  --provenance \
  --provenance-sha "a1b2c3d4e5" \
  --approvals 2
```

Expected output:
```text
+----------------+------------------------------------------------------------------+
|    PROPERTY    |                              VALUE                               |
+----------------+------------------------------------------------------------------+
| Artifact ID    | core-runtime                                                     |
| Decision       | ADMIT                                                            |
| Vector State   | S⟨P:PRESENT, V:POSITIVE, A:NONE, C:COHERENT, E:VERIFIED, L:NORMAL, τ:1⟩ |
| Receipt ID     | rcpt-tw-8d4290b1-5a2f-44cd-8fb9-fa3833c46de9                     |
+----------------+------------------------------------------------------------------+
```

If any mandatory requirement fails (e.g. unsigned artifact, unverified commit SHA in provenance, or fewer than 2 dual-control approvals), the Trustwall immediately issues a `QUARANTINE` decision.

---

## 2. Packaging an Air-Gap Evidence Bundle

Admitted artifacts are packaged with their SBOM, provenance, signatures, and an RFC 9162 Merkle root:

```bash
ggvalet transfer package \
  --artifact-id "core-runtime" \
  --digest "sha256:1111222233334444" \
  --signature "sig-cosign-valid" \
  --sbom "sha256:5555666677778888" \
  --provenance "sha256:9999aaaabbbbcccc"
```

Output:
```text
+-----------------+------------------------------------------------------------------+
|    PROPERTY     |                              VALUE                               |
+-----------------+------------------------------------------------------------------+
| Bundle ID       | bundle-c1ae5d8b-4b3d-4a14-9994-21a86a3f2387                      |
| Artifact ID     | core-runtime                                                     |
| Merkle Root     | 76f62c5a8f5e626fbeedf9dba8346daeca1c9a1fe1a4530e457dced303e6d41d |
| Manifest Digest | a8a7e323a105f9abfc01eceba32a591a23bfc48f799620d9f3cc08fdb0c4fcd0 |
+-----------------+------------------------------------------------------------------+
```

---

## 3. Transmitting Across a Physical Hardware Diode

Transmit the sealed bundle across a unidirectional network diode into the isolated destination network:

```bash
ggvalet transfer diode \
  --artifact-id "core-runtime" \
  --digest "sha256:1111222233334444" \
  --signature "sig-cosign-valid" \
  --sbom "sha256:5555666677778888" \
  --provenance "sha256:9999aaaabbbbcccc" \
  --src "gitlab-shared.local" \
  --dest "gitlab-airgap.local"
```

Output:
```text
✓ Successfully transmitted to isolated destination gitlab-airgap.local.
Status: DELIVERED_AIRGAP
Receipt ID: rcpt-diode-e03df738-4e69-43d0-b0bc-c44945aa22cb
```
The destination registry unpackages the bundle and independently verifies the Merkle root before admitting the artifact.
