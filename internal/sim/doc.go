// Package sim holds the disaster simulation campaign: each test recreates a failure
// that has destroyed real backups (bit rot, deleted packs, ransomware, an unmounted
// volume, a lost manifest, ...) against a real restic repository and records whether
// sapper caught it. scripts/simulate.sh runs the campaign and prints the scoreboard.
package sim
