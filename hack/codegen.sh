#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail
set -x  # Enable command printing for debugging

# Project root directory and other constants
REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "${REPO_ROOT}"

MODULE_NAME="github.com/PulokSaha0706/my-controller"
GROUP="pulok.dev"
VERSION="v1alpha1"
OUTPUT_PKG="pkg/generated"
APIS_PKG="pkg/apis"

# First make sure the target directories exist
mkdir -p "${REPO_ROOT}/${OUTPUT_PKG}/clientset"
mkdir -p "${REPO_ROOT}/${OUTPUT_PKG}/informers"
mkdir -p "${REPO_ROOT}/${OUTPUT_PKG}/listers"

# Install specific version of code-generator for client generation
go get k8s.io/code-generator@v0.24.0

# Get the directory containing the code-generator
CODEGEN_PKG=$(go list -m -f '{{.Dir}}' k8s.io/code-generator@v0.24.0)

# Ensure the scripts are executable
chmod +x "${CODEGEN_PKG}/generate-groups.sh"
chmod +x "${CODEGEN_PKG}/generate-internal-groups.sh"

# Use your existing license file from your project
LICENSE_FILE="${REPO_ROOT}/hack/license/go.txt"
if [ ! -f "${LICENSE_FILE}" ]; then
  echo "License file not found at ${LICENSE_FILE}"
  exit 1
fi

echo "Generating client code..."

# Generate client, informers, listers
"${CODEGEN_PKG}/generate-groups.sh" \
  "client,informer,lister" \
  "${MODULE_NAME}/${OUTPUT_PKG}" \
  "${MODULE_NAME}/${APIS_PKG}" \
  "${GROUP}:${VERSION}" \
  --go-header-file "${LICENSE_FILE}" \
  --output-base "${GOPATH}/src"

# Separately run the deepcopy generator for better control
echo "Generating deepcopy functions..."
"${CODEGEN_PKG}/generate-groups.sh" \
  "deepcopy" \
  "${MODULE_NAME}/${OUTPUT_PKG}" \
  "${MODULE_NAME}/${APIS_PKG}" \
  "${GROUP}:${VERSION}" \
  --go-header-file "${LICENSE_FILE}" \
  --output-base "${GOPATH}/src"

# Check if the code was generated in the expected location
if [ ! -d "${REPO_ROOT}/${OUTPUT_PKG}/clientset" ] || [ -z "$(ls -A ${REPO_ROOT}/${OUTPUT_PKG}/clientset)" ]; then
  echo "Code generation may have placed files in a different location."

  # Check for generated code in the GOPATH directory structure
  TEMP_DIR="${GOPATH}/src/${MODULE_NAME}/${OUTPUT_PKG}"
  if [ -d "${TEMP_DIR}" ]; then
    echo "Found generated code in ${TEMP_DIR}, copying to main project..."

    # Copy the generated code to the correct location
    cp -r "${TEMP_DIR}/clientset"/* "${REPO_ROOT}/${OUTPUT_PKG}/clientset/" || echo "Failed to copy clientset"
    cp -r "${TEMP_DIR}/informers"/* "${REPO_ROOT}/${OUTPUT_PKG}/informers/" || echo "Failed to copy informers"
    cp -r "${TEMP_DIR}/listers"/* "${REPO_ROOT}/${OUTPUT_PKG}/listers/" || echo "Failed to copy listers"
  fi
fi

# Copy deepcopy functions to the API package
DEEPCOPY_SRC="${GOPATH}/src/${MODULE_NAME}/${APIS_PKG}/${GROUP}/${VERSION}/zz_generated.deepcopy.go"
DEEPCOPY_DEST="${REPO_ROOT}/${APIS_PKG}/${GROUP}/${VERSION}/zz_generated.deepcopy.go"

if [ -f "${DEEPCOPY_SRC}" ]; then
  echo "Copying deepcopy functions to API package..."
  cp "${DEEPCOPY_SRC}" "${DEEPCOPY_DEST}" || echo "Failed to copy deepcopy functions"
fi

echo "Code generation completed."
echo "Verifying generated code..."

# Check for client, informers, listers
if [ -d "${REPO_ROOT}/${OUTPUT_PKG}/clientset" ] && \
   [ "$(find "${REPO_ROOT}/${OUTPUT_PKG}/clientset" -type f | wc -l)" -gt 0 ]; then
  echo "✓ Client code generated successfully!"
else
  echo "✗ Client code generation may have failed. Please check for errors."
fi

# Check for deepcopy functions
if [ -f "${DEEPCOPY_DEST}" ]; then
  echo "✓ Deepcopy functions generated successfully!"
else
  echo "✗ Deepcopy functions were not generated. Please check for errors."
fi