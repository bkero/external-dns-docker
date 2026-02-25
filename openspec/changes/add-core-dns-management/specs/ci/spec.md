## ADDED Requirements

### Requirement: CI Pipeline
The system SHALL include a GitHub Actions workflow (`.github/workflows/ci.yml`) that
runs on every pull request and push to `main`. The pipeline SHALL include the following
stages in order: lint, unit tests, integration tests, Docker build.

#### Scenario: PR triggers full CI pipeline
- **WHEN** a pull request is opened against `main`
- **THEN** lint, unit tests, integration tests, and Docker build all run

#### Scenario: Lint failure blocks merge
- **WHEN** golangci-lint reports errors
- **THEN** the CI pipeline fails and the PR cannot be merged

#### Scenario: Unit test failure blocks merge
- **WHEN** any unit test fails
- **THEN** the CI pipeline fails

### Requirement: Multi-Arch Docker Build
The system SHALL build and push a multi-architecture Docker image supporting
`linux/amd64` and `linux/arm64` platforms, using Docker Buildx. Images SHALL be
pushed to a container registry on merge to `main` and on version tags.

#### Scenario: Multi-arch image built on merge to main
- **WHEN** a commit is merged to `main`
- **THEN** a Docker image tagged with the commit SHA is built for both `linux/amd64` and `linux/arm64`

#### Scenario: Release tag triggers versioned image push
- **WHEN** a git tag matching `v*` is pushed
- **THEN** the Docker image is tagged with the version and pushed to the registry

### Requirement: Semantic Release and Versioning
The system SHALL support automatic version tagging on merge to `main` using
conventional commit messages. Version tags SHALL follow semantic versioning (vMAJOR.MINOR.PATCH).

#### Scenario: Feat commit triggers minor version bump
- **WHEN** a commit with message `feat: add feature` is merged to main
- **THEN** the minor version is incremented and a new git tag is created

#### Scenario: Fix commit triggers patch version bump
- **WHEN** a commit with message `fix: correct bug` is merged to main
- **THEN** the patch version is incremented and a new git tag is created
