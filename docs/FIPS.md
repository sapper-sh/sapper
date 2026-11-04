# Building sapper in FIPS 140-3 mode

sapper is not FIPS 140-3 validated, and a FIPS build does not make it so. What a
FIPS build does is link the frozen Go Cryptographic Module v1.0.0, which holds
[CMVP certificate #5247](https://csrc.nist.gov/projects/cryptographic-module-validation-program/certificate/5247),
and run it in FIPS 140-3 mode. Whether that meets a particular requirement is
for the operator and their assessor to decide.

## Build

    GOFIPS140=v1.0.0 CGO_ENABLED=0 go build -trimpath -o sapper-fips ./cmd/sapper
    # or: make fips

`sapper --version` then names the module, for example
`v1.2.0 (FIPS 140-3 mode, Go Cryptographic Module v1.0.0; sapper itself is not FIPS validated)`.
`TestFIPSBuild` builds this way and checks that output.

Reference: [Go, "FIPS 140-3 Compliance"](https://go.dev/doc/security/fips140).

## What is inside the module boundary

- SHA-256, used for file checksums, the manifest Merkle root and the report hash chain.
- Ed25519 key generation, signing and verification for drill reports
  (`keygen`, `--sign-key`, `--pub-key`). The certificate lists EdDSA KeyGen,
  SigGen and SigVer as approved algorithms.
- `crypto/rand`, which in FIPS mode is an SP 800-90A DRBG.

## Caveats

- **restic is outside the boundary.** Backup encryption and authentication are
  done by the separate `restic` binary (AES-256-CTR with Poly1305-AES, scrypt for
  the key). None of it runs in a validated module. A FIPS build of sapper does
  not change how backups are encrypted.
- **Key files are outside the boundary.** Ed25519 private keys are written to and
  read from disk as PEM by sapper. The certificate's caveats include "No
  assurance of the minimum strength of generated SSPs" and none for SSPs that are
  external to the module; protecting the key file (mode `0600`, kept off the
  host where possible) is the operator's job.
- **Only the tested operating environments are covered.** Check the
  certificate's list of operating environments against the host that runs the
  binary.
- `GODEBUG=fips140=only` is a testing aid in Go, not a production mode.
