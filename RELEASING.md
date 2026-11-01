# Releasing

The app ships for macOS, Windows and Linux from the `fansa91/mygo-kgou`
repository. Pushing a tag named `v<version>` (e.g. `v0.1`) runs
`.github/workflows/release.yml`, which builds the three platforms, signs them
when the secrets are set, uploads the installers and update files to a release
of that tag, and then publishes it. Installed apps update to it through the
`updates.github` of `mygo.json`.

1. Bring `main` up to date and make sure the tests pass:

   ```bash
   git pull --rebase origin main
   go test ./internal/...
   ```

2. Add a `## <version>` section at the top of `CHANGELOG.md` describing, in
   Chinese, what changed for people using the app since the last release
   (`git log <last-tag>..HEAD`). The updater shows it in the app's update
   window, and the release page takes it as its notes; `mygo build` fails
   without one, and the workflow refuses to release if the section is missing.
   Do **not** bump `version` in `mygo.json` by hand — the workflow writes it
   from the tag.

3. Commit the changelog, push `main`, then tag and push the tag:

   ```bash
   git push origin main
   git tag v0.1
   git push origin v0.1
   ```

   Push `main` first; pushing the tag before `main` is through starts a release
   build of a commit that isn't on `main`. If a tag went up on the wrong commit,
   cancel its run, delete the tag and the release it made, then tag again:

   ```bash
   git push origin :refs/tags/v0.1
   gh release delete v0.1 --repo fansa91/mygo-kgou --yes
   ```

4. Wait for the release run to finish. The `draft-release` job makes the draft
   and the `build` matrix uploads the three platforms; the `publish` job then
   flips the draft to published:

   ```bash
   gh run watch "$(gh run list --repo fansa91/mygo-kgou --workflow release.yml -L1 --json databaseId -q '.[0].databaseId')" --exit-status
   ```

   If a job fails, fix it and re-run the workflow; the draft-release job skips
   making the release when it already exists, so the rebuild just uploads again
   and the publish job runs once the builds pass.

## Secrets the repository needs

- `MYGO_UPDATER_PRIVATE_KEY` (required): the Ed25519 private key that signs
  updates, matching `updates.publicKey` in `mygo.json`. Without it `mygo build
  -upload` refuses to run, so the builds fail. Set it to the contents of the
  key generated with `go tool mygo keygen` (keep the key out of the repository).
- `MACOS_CERTIFICATE`, `MACOS_CERTIFICATE_PASSWORD`, `APPLE_ID`,
  `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD` (optional): sign and notarize the macOS
  disk image. Without them the macOS build stays unsigned and unsigned updates
  are skipped, but the other platforms build and publish normally.

## Checking the release

- The published release: https://github.com/fansa91/mygo-kgou/releases
- The update feed for a platform, e.g. macOS arm64:

  ```bash
  curl -sL https://github.com/fansa91/mygo-kgou/releases/latest/download/update-darwin-arm64.json
  ```

## The speech recognition libraries

Podcast captions run sherpa-onnx, whose libraries the app downloads with the
model from the `sherpa-onnx-<version>` release of `fansa91/mygo-kgou`, not from
the app's own. They only change with sherpa-onnx's version, in
`scripts/sherpa`. Then pack them again, signing the macOS ones with the app's
Developer ID (the app's hardened runtime loads no others), commit the
`internal/speech/natives.go` it writes, and upload them as a prerelease, which
never becomes the latest that updates look for:

```bash
go run ./scripts/sherpa -sign "Developer ID Application: xinyu zeng (GJE9R5VE87)"
gh release create sherpa-onnx-2.0.1 --repo fansa91/mygo-kgou --prerelease --latest=false build/sherpa/*
```
