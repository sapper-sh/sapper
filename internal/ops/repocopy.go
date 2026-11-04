package ops

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sapper-sh/sapper/internal/manifest"
	"github.com/sapper-sh/sapper/internal/report"
	"github.com/sapper-sh/sapper/internal/restic"
)

// Optional capabilities of a Restic client. The restic package implements all of them;
// test fakes may implement only some, and ops falls back to the older behaviour.
type (
	snapshotLister interface {
		Snapshots(ctx context.Context, tag string) ([]restic.Snapshot, error)
	}
	repoChecker interface {
		RepoExists(ctx context.Context) (bool, error)
	}
	idForgetter interface {
		ForgetIDs(ctx context.Context, ids []string) error
	}
	blobStore interface {
		SaveBlob(ctx context.Context, data []byte, tags []string) (string, error)
		LoadBlob(ctx context.Context, id string, max int64) ([]byte, error)
	}
	lockLister interface {
		Locks(ctx context.Context) (int, error)
	}
)

// ManifestTag marks the snapshots that hold manifest copies; forTag links one to the
// snapshot it describes.
const ManifestTag = "sapper-manifest"

func forTag(id string) string { return "for:" + id }

// maxManifestCopy bounds how much of a manifest copy is read back from the repository.
const maxManifestCopy = 256 << 20

// manifestCopy is what sapper stores in the repository next to each snapshot, so a drill
// from a machine that never saw the backup still has checksums to compare against. restic
// encrypts and authenticates it with the repository key; the optional ed25519 signature
// also proves it came from the holder of the signing key, not just of the password.
type manifestCopy struct {
	Schema    int             `json:"schema"`
	Snapshot  string          `json:"snapshot"`
	SHA256    string          `json:"sha256"`
	Manifest  json.RawMessage `json:"manifest"`
	KeyID     string          `json:"key_id,omitempty"`
	PublicKey string          `json:"public_key,omitempty"`
	Signature string          `json:"signature,omitempty"`
}

const copyPrefix = "sapper-manifest-v1\n"

func signedBytes(snapshot, sum string) []byte {
	return []byte(copyPrefix + snapshot + "\n" + sum)
}

// ErrManifestSignature is returned when a manifest copy is unsigned or signed by the wrong key
// while a public key was required.
var ErrManifestSignature = errors.New("manifest copy signature")

func encodeCopy(id string, m *manifest.Manifest, key ed25519.PrivateKey) ([]byte, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	c := manifestCopy{Schema: 1, Snapshot: id, SHA256: hex.EncodeToString(sum[:]), Manifest: raw}
	if key != nil {
		pub := key.Public().(ed25519.PublicKey)
		c.KeyID = report.KeyID(pub)
		c.PublicKey = base64.StdEncoding.EncodeToString(pub)
		c.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, signedBytes(id, c.SHA256)))
	}
	return json.Marshal(c)
}

// decodeCopy checks a manifest copy belongs to snapshot id, is intact and, when pub is set,
// was signed by pub.
func decodeCopy(data []byte, id string, pub ed25519.PublicKey) (*manifest.Manifest, bool, error) {
	var c manifestCopy
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&c); err != nil {
		return nil, false, fmt.Errorf("manifest copy: %w", err)
	}
	if c.Snapshot != id {
		return nil, false, fmt.Errorf("manifest copy is for snapshot %s, not %s", short(report.Clean(c.Snapshot)), short(id))
	}
	sum := sha256.Sum256(c.Manifest)
	if hex.EncodeToString(sum[:]) != c.SHA256 {
		return nil, false, errors.New("manifest copy checksum does not match its contents")
	}
	signed := false
	if c.Signature != "" {
		pk, err1 := base64.StdEncoding.DecodeString(c.PublicKey)
		sig, err2 := base64.StdEncoding.DecodeString(c.Signature)
		if err1 != nil || err2 != nil || len(pk) != ed25519.PublicKeySize || !ed25519.Verify(pk, signedBytes(id, c.SHA256), sig) {
			return nil, false, fmt.Errorf("%w: invalid", ErrManifestSignature)
		}
		if pub != nil && !bytes.Equal(pk, pub) {
			return nil, false, fmt.Errorf("%w: signed by key %s, not the expected %s", ErrManifestSignature, report.KeyID(pk), report.KeyID(pub))
		}
		signed = true
	} else if pub != nil {
		return nil, false, fmt.Errorf("%w: copy is not signed, but a public key was given", ErrManifestSignature)
	}
	m, err := manifest.Parse(c.Manifest)
	if err != nil {
		return nil, false, fmt.Errorf("manifest copy: %w", err)
	}
	return m, signed, nil
}

// saveCopy stores the manifest of snapshot id in the repository.
func saveCopy(ctx context.Context, r Restic, fortress, id string, m *manifest.Manifest, key ed25519.PrivateKey) (string, error) {
	bs, ok := r.(blobStore)
	if !ok {
		return "", nil
	}
	data, err := encodeCopy(id, m, key)
	if err != nil {
		return "", err
	}
	return bs.SaveBlob(ctx, data, []string{ManifestTag, forTag(id), "fortress:" + fortress})
}

// loadCopy reads the manifest copy for snapshot id from the repository. found is false
// when the client cannot store copies or the repository holds none for id.
func loadCopy(ctx context.Context, r Restic, id string, pub ed25519.PublicKey) (m *manifest.Manifest, signed, found bool, err error) {
	bs, ok1 := r.(blobStore)
	sl, ok2 := r.(snapshotLister)
	if !ok1 || !ok2 {
		return nil, false, false, nil
	}
	snaps, err := sl.Snapshots(ctx, forTag(id))
	if err != nil {
		return nil, false, false, err
	}
	var copies []restic.Snapshot
	for _, s := range snaps {
		if hasTag(s, ManifestTag) && validID(s.ID) {
			copies = append(copies, s)
		}
	}
	if len(copies) == 0 {
		return nil, false, false, nil
	}
	// sapper writes one copy per snapshot. More than one means someone with the password
	// added another; if they disagree, neither can be trusted without a signing key.
	var sum string
	for i := len(copies) - 1; i >= 0 && i >= len(copies)-maxCopies; i-- {
		data, err := bs.LoadBlob(ctx, copies[i].ID, maxManifestCopy)
		if err != nil {
			return nil, false, true, err
		}
		cm, csigned, err := decodeCopy(data, id, pub)
		if err != nil {
			return nil, false, true, err
		}
		if m == nil {
			m, signed, sum = cm, csigned, cm.Sum()
		} else if cm.Sum() != sum {
			return nil, false, true, fmt.Errorf("%w: the repository holds %d different manifest copies for this snapshot; someone with the repository password added one. Drill with --pub-key to accept only signed copies",
				ErrManifestConflict, len(copies))
		}
	}
	return m, signed, true, nil
}

// maxCopies bounds how many manifest copies for one snapshot are read.
const maxCopies = 5

// ErrManifestConflict is returned when the repository holds manifest copies that disagree.
var ErrManifestConflict = errors.New("conflicting manifest copies")

func hasTag(s restic.Snapshot, tag string) bool {
	for _, t := range s.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

func copyTarget(s restic.Snapshot) string {
	for _, t := range s.Tags {
		if id, ok := strings.CutPrefix(t, "for:"); ok {
			return id
		}
	}
	return ""
}
