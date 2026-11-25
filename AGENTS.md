# Development rules

- Target: Delphi applications on Windows amd64. Linux tests validate the portable core only.
- Continue implementation autonomously using the original AntiPessimizer as the behavioral reference. Do not request intermediate manual validation from the user.
- Work on a branch and open a PR. Never push implementation changes directly to main.
- Before merging the exact PR head, require passing Linux core checks and Windows checks, including ETW integration and native viewer smoke. Fix genuine behavior failures; do not weaken tests to make CI pass.
- Add regression tests for defects, run gofmt, go vet, tests and race checks appropriate to the change. Cross-compile/vet Windows code before pushing a branch.
- Keep measured evidence distinct from unsupported formats and untested Delphi compiler versions. Do not claim instrumentation metrics from sampling.

## Owner attribution

- All assisted coding belongs to the user, Gabriel Teiga (`gabrielteiga`). Use the owner's verified Git identity for author and committer.
- Never attribute commits, signatures or automatic coauthor trailers to assistants, models, tools or providers.
- At the start of every coding session, run `scripts/configure-owner-attribution.sh`. Run `.githooks/owner-policy identity` before committing and `.githooks/owner-policy commit HEAD` before pushing.
- Keep the ownership hooks enabled. Correct a rejected identity or attribution at its source and retry with the owner's identity.
- Preserve this rule in session handoffs and across branches. Fresh environments must configure attribution before their first commit.
