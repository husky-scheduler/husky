# Contributing to Husky

Thanks for contributing to Husky.

## Before opening a pull request
- discuss significant changes in an issue first when possible
- keep changes focused and small
- add or update tests for behavior changes
- update docs when public behavior changes
- avoid unrelated refactors in the same pull request

## Development workflow
1. Fork the repository and create a branch.
2. Make the change.
3. Run the relevant checks locally.
4. Update documentation if needed.
5. Open a pull request with a clear description.

## Local validation
At minimum, run the checks relevant to the change:

```bash
make build
go test ./...
```

If the change affects docs or the dashboard, also validate those locally.

## Pull request expectations
Please include:
- what changed
- why it changed
- how it was tested
- screenshots or terminal output when UI or UX changed
- any follow-up work or known limitations

## Commit guidance
- use clear commit messages
- prefer a small series of focused commits
- keep history readable for review

## Release process
Releases are automated from Git tags.

To create a release, push a version tag matching `v*`, for example:

```bash
git tag v0.1.0-alpha.1
git push origin v0.1.0-alpha.1
```

That triggers the workflow in [.github/workflows/release.yml](.github/workflows/release.yml).

## Reporting security issues
Do not open public issues for suspected security vulnerabilities.

Instead, follow the policy in [SECURITY.md](SECURITY.md).
