#!/usr/bin/env bash

set -e -u -o pipefail

if [ -z "${1:-}" ]; then
    echo "Usage: $0 <directory> [url]" >&2
    exit 1
fi

DIR="${1}"
URL="${2:-http://localhost:3000/api/receiver/upload}"

if [ ! -d "${DIR}" ]; then
    echo "Error: directory does not exist: ${DIR}" >&2
    exit 1
fi

shopt -s nullglob
FILES=("${DIR}"/*)

if [ ${#FILES[@]} -eq 0 ]; then
    echo "No files found in ${DIR}" >&2
    exit 1
fi

echo "Found ${#FILES[@]} files in ${DIR}"

for FILE in "${FILES[@]}"; do
    if [ -f "${FILE}" ]; then
        echo "Uploading: ${FILE}"
        curl -X POST -F "files=@${FILE}" "${URL}" || {
            echo "Failed to upload ${FILE}" >&2
            exit 1
        }
    fi
done

echo "All files uploaded successfully"
