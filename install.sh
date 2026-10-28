#!/bin/bash
set -e

REPO="iq2i/ainspector"
BINARY_NAME="ainspector"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Linux*)  echo "linux" ;;
        Darwin*) echo "darwin" ;;
        *)       echo "unsupported" ;;
    esac
}

# Detect architecture
detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *)             echo "unsupported" ;;
    esac
}

# Get latest version from GitHub API
get_latest_version() {
    curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/'
}

OS=$(detect_os)
ARCH=$(detect_arch)
VERSION="${VERSION:-$(get_latest_version)}"

if [ "$OS" = "unsupported" ] || [ "$ARCH" = "unsupported" ]; then
    echo "Error: Unsupported OS ($OS) or architecture ($ARCH)"
    exit 1
fi

ARCHIVE_NAME="${BINARY_NAME}-${OS}-${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE_NAME}"

echo "Downloading ${BINARY_NAME} ${VERSION} for ${OS}/${ARCH}..."

TMP_DIR=$(mktemp -d)
trap "rm -rf ${TMP_DIR}" EXIT

curl -fsSL "${DOWNLOAD_URL}" -o "${TMP_DIR}/${ARCHIVE_NAME}"

# Check the archive against the checksums published with the release
if ! curl -fsSL "https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt" -o "${TMP_DIR}/checksums.txt"; then
    echo "Error: no checksums.txt published with ${VERSION}, refusing to install an unverified archive"
    exit 1
fi
EXPECTED=$(grep " ${ARCHIVE_NAME}\$" "${TMP_DIR}/checksums.txt" | awk '{print $1}')
if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL=$(sha256sum "${TMP_DIR}/${ARCHIVE_NAME}" | awk '{print $1}')
else
    ACTUAL=$(shasum -a 256 "${TMP_DIR}/${ARCHIVE_NAME}" | awk '{print $1}')
fi
if [ -z "${EXPECTED}" ] || [ "${EXPECTED}" != "${ACTUAL}" ]; then
    echo "Error: checksum mismatch for ${ARCHIVE_NAME}"
    exit 1
fi

tar -xzf "${TMP_DIR}/${ARCHIVE_NAME}" -C "${TMP_DIR}"
chmod +x "${TMP_DIR}/${BINARY_NAME}"

if [ -w "${INSTALL_DIR}" ]; then
    mv "${TMP_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/"
else
    sudo mv "${TMP_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/"
fi

echo "${BINARY_NAME} ${VERSION} installed to ${INSTALL_DIR}/${BINARY_NAME}"
