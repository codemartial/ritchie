# Agent guide

`README.md` describes the repository, the build, and the licences. This
file adds what an agent needs beyond it.

- Questions about the Ritchie language are answered from `skill/SKILL.md`,
  whose language capsule describes what the compiler accepts. The language
  reference under `docs/ritchie-reference/` states the full meaning of each
  form.
- Check Ritchie code with the compiler in this repository. Install it once
  from the repository root as `README.md` shows. Then use `rcrun` for one-time
  execution or build an executable using `rcc` or `ritchie`.
- The compiler module is `src/`, and `go.work` lets Go commands run from the
  repository root. Run `go -C src vet ./...` and `go -C src test ./...` after
  changing Go code. The module depends on the Go standard library only.
- `docs/ritchie-reference.pdf` is rendered from `docs/ritchie-reference/`
  at each release, so edits are made to the Typst sources.
- Changes keep within the licences that `README.md` lists.
