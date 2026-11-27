# Development rules

- Target: Delphi applications on Windows amd64. Linux tests validate the portable core only.
- Continue implementation autonomously using the original AntiPessimizer as the behavioral reference. Do not request intermediate manual validation from the user.
- Work on a branch and open a PR. Never push implementation changes directly to main.
- Before merging the exact PR head, require passing Linux core checks and Windows checks, including ETW integration and native viewer smoke. The Go workflow runs once per PR candidate, verifies its exact checkout and base ancestry, then runs again on each main merge. Authorship checks remain on every push and PR. Fix genuine behavior failures; do not weaken tests to make CI pass.
- Group related implementation and regression tests into complete functional blocks. Do not rerun suites after each trivial edit; batch trivial changes and defer local execution until a complex block or a meaningful verification point. Run appropriate checks for complex changes (especially ABI, unwind, exception handling and concurrency), including Windows cross-vet/build. Required exact-head CI and post-merge checks remain mandatory.
- Keep measured evidence distinct from unsupported formats and untested Delphi compiler versions. Do not claim instrumentation metrics from sampling.

## Owner attribution

- All assisted coding belongs to the user, Gabriel Teiga (`gabrielteiga`). Use the owner's verified Git identity for author and committer.
- Never attribute commits, signatures or automatic coauthor trailers to assistants, models, tools or providers.
- At the start of every coding session, run `scripts/configure-owner-attribution.sh`. Run `.githooks/owner-policy identity` before committing and `.githooks/owner-policy commit HEAD` before pushing.
- Configure `user.useConfigOnly=true`. Check `git var GIT_AUTHOR_IDENT` and `git var GIT_COMMITTER_IDENT` before the first commit of each session; verify the created SHA and trailers before publishing.
- Keep the ownership hooks enabled. Correct a rejected identity or attribution at its source and retry with the owner's identity.
- Preserve this rule in session handoffs and across branches. Fresh environments must configure attribution before their first commit.
