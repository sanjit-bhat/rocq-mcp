rules:
- NEVER chain shell commands with && or ; or |.
  run each command as its own separate Bash call.
  this ensures each call matches an allowed permission pattern (e.g. `git *`).
- NEVER use Bash for file reading/searching. use Read, Grep, Glob instead.
  these are already allowed and don't require approval.
- NEVER use `cat`, `head`, `tail`, `ls`, `find`, `grep`, `rg` in Bash.
- clone exploratory code into `/tmp`.
i've given permission for you to read that dir.
- we're using github.com/modelcontextprotocol/go-sdk.
- commit checkpoints. write concise, descriptive commits.
- update your plan.md whenever you're done with something. commit that.
- test often to make sure you're on the right track.
