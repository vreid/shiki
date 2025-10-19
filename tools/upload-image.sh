#!/usr/bin/env bash

set -e -u -o pipefail

IMAGE_PATH="${1:?Usage: $0 <image-path> [host:port]}"
HOST_PORT="${2:-localhost:50051}"

if [ ! -f "${IMAGE_PATH}" ]; then
    echo "Error: File not found: ${IMAGE_PATH}" >&2
    exit 1
fi

base64 -w 0 "${IMAGE_PATH}" |
    jq -Rs '{image_data: .}' |
    grpcurl -plaintext \
        -d @ \
        "${HOST_PORT}" \
        image_processor.v1alpha1.ImageProcessorService/UploadImage
