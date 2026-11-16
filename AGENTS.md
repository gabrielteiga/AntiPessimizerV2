# Development rules

- Target: Delphi applications on Windows amd64. Linux tests validate the portable core only.
- Continue implementation autonomously using the original AntiPessimizer as the behavioral reference. Do not request intermediate manual validation from the user.
- Work on a branch and open a PR. Never push implementation changes directly to main.
- Before merging the exact PR head, require passing Linux core checks and Windows checks, including ETW integration and native viewer smoke. Fix genuine behavior failures; do not weaken tests to make CI pass.
- Add regression tests for defects, run gofmt, go vet, tests and race checks appropriate to the change. Cross-compile/vet Windows code before pushing a branch.
- Keep measured evidence distinct from unsupported formats and untested Delphi compiler versions. Do not claim instrumentation metrics from sampling.
