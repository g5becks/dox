# Release Setup Guide

This guide explains how to set up automated releases for dox using goreleaser and GitHub Actions.

## Prerequisites

1. **GitHub Repository Secrets**: You'll need to configure several secrets in your GitHub repository settings.

2. **External Accounts** (optional, for package distribution):
   - Homebrew tap repository
   - AUR (Arch User Repository) account
   - Chocolatey account
   - GitHub Container Registry (GHCR) access

## GitHub Secrets Configuration

Navigate to your repository settings → Secrets and variables → Actions → Repository secrets.

### Required Secrets

#### `HOMEBREW_TAP_TOKEN`
**Purpose**: Allows goreleaser to push formula updates to your Homebrew tap repository.

**Setup:**
1. Create a GitHub Personal Access Token (classic):
   - Go to GitHub Settings → Developer settings → Personal access tokens → Tokens (classic)
   - Click "Generate new token (classic)"
   - Name: `dox-homebrew-tap`
   - Scopes: `repo` (full control)
   - Generate and copy the token

2. Create the tap repository:
   ```bash
   # On GitHub, create a new repo named "homebrew-tap"
   # URL will be: https://github.com/g5becks/homebrew-tap
   ```

3. Add the secret:
   - Name: `HOMEBREW_TAP_TOKEN`
   - Value: Your generated PAT

#### `AUR_KEY` (Optional)
**Purpose**: Publishes to Arch User Repository.

**Setup:**
1. Create an AUR account at https://aur.archlinux.org/register/

2. Generate SSH key for AUR:
   ```bash
   ssh-keygen -t ed25519 -C "aur@g5becks" -f ~/.ssh/aur
   ```

3. Add public key to AUR:
   - Login to AUR
   - Go to "My Account"
   - Add `~/.ssh/aur.pub` content to SSH Public Keys

4. Get the private key:
   ```bash
   cat ~/.ssh/aur
   ```

5. Add the secret:
   - Name: `AUR_KEY`
   - Value: The entire private key (including `-----BEGIN` and `-----END` lines)

#### `CHOCOLATEY_API_KEY` (Optional)
**Purpose**: Publishes to Chocolatey (Windows package manager).

**Setup:**
1. Create account at https://community.chocolatey.org/

2. Get your API key:
   - Login to Chocolatey
   - Go to your account settings
   - Copy the API key

3. Add the secret:
   - Name: `CHOCOLATEY_API_KEY`
   - Value: Your Chocolatey API key

**Note**: First package submission requires manual moderation. Subsequent releases are automatic.

### Automatic Secrets

- **`GITHUB_TOKEN`**: Automatically provided by GitHub Actions, no setup needed.
- **GHCR Access**: Uses `GITHUB_TOKEN` for GitHub Container Registry.

## Package Distribution Overview

### What Gets Published

When you push a new tag (e.g., `v1.0.0`), goreleaser will:

1. **GitHub Release**: Create release with changelog and binaries
   - Linux: amd64, arm64, armv7
   - macOS: Universal binary (amd64 + arm64)
   - Windows: amd64
   - FreeBSD: amd64, arm64

2. **Package Formats**:
   - `.tar.gz` / `.zip` archives
   - `.deb` (Debian/Ubuntu)
   - `.rpm` (RedHat/Fedora/CentOS)
   - `.apk` (Alpine Linux)
   - Arch Linux packages

3. **Homebrew** (with `HOMEBREW_TAP_TOKEN`):
   - Formula pushed to `g5becks/homebrew-tap`
   - Users install with: `brew install g5becks/tap/dox`

4. **AUR** (with `AUR_KEY`):
   - Package pushed to https://aur.archlinux.org/packages/dox-bin
   - Users install with: `yay -S dox-bin` or `paru -S dox-bin`

5. **Chocolatey** (with `CHOCOLATEY_API_KEY`):
   - Package pushed to Chocolatey Community Repository
   - Users install with: `choco install dox`
   - **First release requires manual approval** (24-48 hours)

6. **Docker** (automatic):
   - Multi-arch images (amd64, arm64)
   - Published to `ghcr.io/g5becks/dox`
   - Tags: `latest`, `v1.0.0`
   - Users run with: `docker pull ghcr.io/g5becks/dox:latest`

## Making a Release

### 1. Ensure Everything is Ready

```bash
# All tests passing
go test ./...

# Code is clean
golangci-lint run ./...

# Main branch is up to date
git checkout main
git pull origin main
```

### 2. Tag the Release

```bash
# Tag with semantic version
git tag -a v1.0.0 -m "Release v1.0.0"

# Push tag to trigger release
git push origin v1.0.0
```

### 3. Monitor the Release

1. Watch the GitHub Actions workflow:
   - Go to https://github.com/g5becks/dox/actions
   - Click on the "Release" workflow run

2. Check the release page after completion:
   - https://github.com/g5becks/dox/releases

3. Verify installations:
   ```bash
   # Homebrew (if configured)
   brew install g5becks/tap/dox
   dox --version

   # Docker (always available)
   docker run --rm ghcr.io/g5becks/dox:v1.0.0 --version
   ```

## Troubleshooting

### Homebrew Issues

**Problem**: Tap update fails
- Check `HOMEBREW_TAP_TOKEN` has `repo` scope
- Ensure `homebrew-tap` repository exists and is public
- Check workflow logs for specific error

### AUR Issues

**Problem**: AUR push fails
- Verify SSH key is correct (check secret format)
- Ensure AUR account is active
- Check that `dox-bin` package doesn't conflict with existing package

### Chocolatey Issues

**Problem**: First release stuck in moderation
- **This is normal** — first submission requires manual review (24-48 hours)
- Check email for moderator feedback
- Subsequent releases are automatic

**Problem**: API key rejected
- Regenerate key in Chocolatey account settings
- Update `CHOCOLATEY_API_KEY` secret

### Docker Issues

**Problem**: Image push fails
- `GITHUB_TOKEN` permissions should be automatic
- Check if GitHub Packages is enabled for the repository
- Verify Dockerfile exists at repository root

## Testing Locally

Test the release process without publishing:

```bash
# Install goreleaser
go install github.com/goreleaser/goreleaser/v2@latest

# Run release in snapshot mode (no publish)
goreleaser release --snapshot --clean

# Check dist/ for artifacts
ls -la dist/
```

## Minimal Setup (No External Services)

If you only want GitHub releases and binaries:

1. Remove or comment out these sections in `.goreleaser.yaml`:
   - `brews` (Homebrew)
   - `aurs` (AUR)
   - `chocolateys` (Chocolatey)
   - `dockers` (Docker)

2. Only `GITHUB_TOKEN` will be used (automatic)

3. Users download from releases page:
   ```bash
   curl -L https://github.com/g5becks/dox/releases/latest/download/dox_Linux_x86_64.tar.gz | tar xz
   ```

## Summary

**Minimum viable setup**: Just push a tag — GitHub releases will work automatically.

**Full setup**: Configure all secrets → get Homebrew, AUR, Chocolatey, and Docker distribution.

**Recommendation**: Start with GitHub releases only, add package managers as needed.
