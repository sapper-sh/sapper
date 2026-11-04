# Signing-key lifecycle

Drill reports can be signed with an ed25519 key. A signature is worth only as
much as the key's protection, so treat the key as having a life: created,
used, rotated, and sometimes revoked.

| Stage | What to do | sapper support |
|---|---|---|
| Create | `sapper keygen --out ~/.config/sapper/drill-2026.key`. Put the year or a counter in the name. | Writes `0600` with `O_EXCL`; never overwrites; never prints the private key; prints the key ID. |
| Record | Copy the `.pub` file and the key ID somewhere the drill host cannot write. | `key id` in `keygen` output; `KeyID` is the first 16 hex characters of the sha256 of the public key. |
| Use | `sapper breach --sign-key <key>`; verify with `sapper reports verify --pub-key <key>.pub`. | Every report records the signing key ID. |
| Rotate | Create a new key with a new path, switch `--sign-key` to it, keep the old `.pub`. Verify across the change with both keys: `--pub-key old.pub --pub-key new.pub`. | `--pub-key` is repeatable; a report must be signed by one of the given keys. |
| Revoke | If a private key may have leaked, add its key ID to a revocation file and pass it to every verification: `sapper reports verify --revoked revoked.txt`. Then rotate and run a fresh `sapper breach`. | Reports signed by a revoked key fail verification (exit 3), whatever date they carry, because a leaked key can sign any date. Passing a revoked key to `--pub-key` is a usage error (exit 2). |
| Retire | Keep retired public keys for as long as you keep reports they signed. | Nothing to do; old reports keep their key ID. |

Revocation file format: one key ID per line; `#` starts a comment.

    # 2026-11-04  laptop lost; rotated to drill-2026b
    0123456789abcdef

Revocation invalidates every report the key signed, including honest ones made
before the leak. That is deliberate: sapper cannot tell which ones are honest.
The hash chain and any `--head` you recorded elsewhere still show whether the
report contents were changed, and a new drill signed by the new key gives a
fresh result.
