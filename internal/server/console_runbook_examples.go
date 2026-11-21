package server

// Example runbooks for the disk that keeps filling up on a build server (design note 051, the Jenkins demo). They
// are offered when making a runbook, to read, change and have reviewed; none is installed by itself.

type runbookExample struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Kind        string         `json:"kind"`
	Script      string         `json:"script"`
	Params      []RunbookParam `json:"params"`
	Timeout     int            `json:"timeout_seconds"`
}

var runbookExamples = []runbookExample{
	{Name: "Disk report", Kind: rbCheck, Timeout: 120,
		Description: "Free space, the largest folders, Docker's use and the newest backups. Only reads.",
		Params: []RunbookParam{
			{Name: "SCAN_DIR", Label: "Folder to measure", Type: "choice", Choices: []string{"/var/lib/jenkins", "/var/lib/docker", "/var", "/home", "/"}, Default: "/var/lib/jenkins"},
			{Name: "BACKUP_DIR", Label: "Backup folder", Type: "choice", Choices: []string{"/var/backups/jenkins", "/var/backups/db", "/backup"}, Default: "/var/backups/jenkins"},
		},
		Script: `set -u
echo "== Disk"
df -hP -x tmpfs -x devtmpfs -x overlay
echo
echo "== Largest folders in $SCAN_DIR"
du -xh --max-depth=2 "$SCAN_DIR" 2>/dev/null | sort -rh | head -n 15
if command -v docker >/dev/null 2>&1; then
  echo
  echo "== Docker"
  docker system df
fi
if [ -d "$BACKUP_DIR" ]; then
  echo
  echo "== Newest backups in $BACKUP_DIR"
  ls -lt --time-style=+%Y-%m-%d "$BACKUP_DIR" | head -n 15
  echo "Files: $(find "$BACKUP_DIR" -maxdepth 1 -type f | wc -l), size: $(du -sh "$BACKUP_DIR" | cut -f1)"
fi`},
	{Name: "Docker cleanup", Kind: rbFix, Timeout: 900,
		Description: "Removes stopped containers, dangling images and build cache older than a number of hours; optionally every unused image.",
		Params: []RunbookParam{
			{Name: "OLDER_THAN_HOURS", Label: "Older than (hours)", Type: "int", Min: 1, Max: 720, Default: "24"},
			{Name: "UNUSED_IMAGES", Label: "Also remove every unused image", Type: "choice", Choices: []string{"no", "yes"}, Default: "no"},
		},
		Script: `set -eu
echo "Before: $(df -hP /var/lib/docker | tail -n 1)"
docker container prune --force --filter "until=${OLDER_THAN_HOURS}h"
docker image prune --force --filter "until=${OLDER_THAN_HOURS}h"
if [ "$UNUSED_IMAGES" = "yes" ]; then
  docker image prune --all --force --filter "until=${OLDER_THAN_HOURS}h"
fi
docker builder prune --force --filter "until=${OLDER_THAN_HOURS}h"
echo "After:  $(df -hP /var/lib/docker | tail -n 1)"`},
	{Name: "Old backups", Kind: rbFix, Timeout: 600,
		Description: "Deletes backup files older than a number of days from one folder, always keeping the newest few.",
		Params: []RunbookParam{
			{Name: "BACKUP_DIR", Label: "Backup folder", Type: "choice", Choices: []string{"/var/backups/jenkins", "/var/backups/db", "/backup"}, Default: "/var/backups/jenkins"},
			{Name: "EXTENSION", Label: "Backup files end in", Type: "choice", Choices: []string{"sql.gz", "dump", "tar.gz", "zip", "bak"}, Default: "tar.gz"},
			{Name: "KEEP", Label: "Always keep the newest", Type: "int", Min: 1, Max: 100, Default: "5"},
			{Name: "OLDER_THAN_DAYS", Label: "Delete when older than (days)", Type: "int", Min: 1, Max: 365, Default: "14"},
		},
		Script: `set -eu
cd "$BACKUP_DIR"
echo "Before: $(df -hP . | tail -n 1)"
removed=0
# The newest $KEEP files are never removed, whatever their age.
ls -1t -- *."$EXTENSION" 2>/dev/null | tail -n +$((KEEP + 1)) | while IFS= read -r f; do
  if [ -n "$(find . -maxdepth 1 -name "$f" -mtime +"$OLDER_THAN_DAYS" -print)" ]; then
    echo "removing $f"
    rm -f -- "$f"
  fi
done
echo "After:  $(df -hP . | tail -n 1)"`},
}
