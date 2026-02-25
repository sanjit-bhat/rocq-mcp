rules:
- NEVER chain shell commands with && or ; or |.
  run each command as its own separate Bash call.
  this ensures each call matches an allowed permission pattern (e.g. `git *`).
- NEVER use Bash for file reading/searching. use Read, Grep, Glob instead.
  these are already allowed and don't require approval.
- clone exploratory code into `/tmp`.
i've given permission for you to read that dir.
- commit checkpoints. write concise, descriptive commits.
  add yourself as a co-author.
- write commit messages to a fresh random file in `/tmp`,
  then run `git commit -F {file_path}`.
  this avoids multi-line shell quoting issues with the `Bash(git *)` permission pattern.
- test often to make sure you're on the right track.
