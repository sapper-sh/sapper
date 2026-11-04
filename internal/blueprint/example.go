package blueprint

// Example is the blueprint written by `sapper init`.
const Example = `# fortress.blueprint
version: 1
fortress: my-fortress

# Host directory for app data. Relative paths resolve against this file's directory.
data_root: ./data

apps:
  vaultwarden:
    port: 8081
    volumes:
      data: vaultwarden
  filebrowser:
    port: 8082
    volumes:
      files: filebrowser/files
      database: filebrowser/database
  memos:
    port: 8083
    volumes:
      data: memos

backup:
  # Local path or any restic repository string (sftp:, s3:, rest:, ...).
  repository: ./backups/restic
  # Name of the environment variable holding the repository password.
  password_env: SAPPER_RESTIC_PASSWORD
  keep_last: 14

drill:
  # cron expression for scheduled breach drills
  schedule: "30 3 * * 0"
`
