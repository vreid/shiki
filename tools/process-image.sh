#!/usr/bin/env bash

set -e -u -o pipefail

error_exit() {
    jq -n --arg error "${1:?}" '{"error": $error}'
    exit 0
}

if [ -z "${1:-}" ]; then
    error_exit "no input file specified"
fi

INPUT="${1}"

if [ ! -f "${INPUT}" ]; then
    error_exit "input file does not exist"
fi

TEMP="$(mktemp -d -p .)" ||
    error_exit "failed to create temp directory"
trap 'rm -rf "${TEMP}"' EXIT

SHA256=$(sha256sum "${INPUT}" | awk '{print $1}') ||
    error_exit "failed to compute sha256"

cp "${INPUT}" "${TEMP}/${SHA256}" ||
    error_exit "failed to copy input file"

cd "${TEMP}"

INPUT="${SHA256}"

CONVERT_ERR=$(magick convert "${INPUT}" -strip "${SHA256}_strip" 2>&1) ||
    error_exit "failed to strip metadata from input: ${CONVERT_ERR}"

SHA256_STRIP=$(sha256sum "${SHA256}_strip" | awk '{print $1}') ||
    error_exit "failed to compute sha256 of stripped image"

mv "${SHA256}_strip" "${SHA256_STRIP}" ||
    error_exit "failed to rename stripped image"

CONVERT_ERR=$(magick convert "${SHA256}" -strip "${SHA256}.webp" 2>&1) ||
    error_exit "failed to convert to webp: ${CONVERT_ERR}"

SHA256_WEBP="$(sha256sum "${SHA256}.webp" | awk '{print $1}')" ||
    error_exit "failed to compute sha256 of webp"

UUID="$(uuidgen -s -N "${SHA256_WEBP}" -n @oid)" ||
    error_exit "failed to generate uuid"

if [ -f "../${UUID}.tar.xz" ]; then
    jq -n --arg success "${UUID}" '{"success": $success}'
    exit 0
fi

mv "${SHA256}.webp" "${UUID}.webp" ||
    error_exit "failed to rename webp"

CONVERT_ERR=$(magick convert "${UUID}.webp" \
    -colors 16 \
    -depth 8 \
    -format "%c" \
    histogram:info:- 2>&1) ||
    error_exit "failed to generate histogram: ${CONVERT_ERR}"

echo "${CONVERT_ERR}" | sort -rn >"${UUID}.hist"

CONVERT_ERR=$(magick convert "${UUID}.webp" \
    -resize 1024x1024 \
    -gravity center \
    -background transparent \
    -extent 1024x1024 \
    -strip \
    "${UUID}_1024.webp" 2>&1) ||
    error_exit "failed to resize to 1024x1024: ${CONVERT_ERR}"

QUALITY="50"
for SIZE in 1024 512 256 128 64 32; do
    CONVERT_ERR=$(magick convert "${UUID}_1024.webp" \
        -resize ${SIZE}x${SIZE} \
        -quality "${QUALITY}" \
        "${UUID}_${SIZE}_${QUALITY}.webp" 2>&1) ||
        error_exit "failed to generate ${SIZE}x${SIZE} variant: ${CONVERT_ERR}"
done

EXIF_ERR=$(exiftool -json "${INPUT}" 2>&1 >"${SHA256}.json") ||
    error_exit "failed to extract exif from input: ${EXIF_ERR}"

EXIF_ERR=$(exiftool -json "${SHA256_STRIP}" 2>&1 >"${SHA256_STRIP}.json") ||
    error_exit "failed to extract exif from stripped: ${EXIF_ERR}"
EXIF_ERR=$(exiftool -json "${UUID}.webp" 2>&1 >"${UUID}.json") ||
    error_exit "failed to extract exif from webp: ${EXIF_ERR}"

EXIF_ERR=$(exiftool -json "${UUID}_1024.webp" 2>&1 >"${UUID}_1024.json") ||
    error_exit "failed to extract exif from 1024: ${EXIF_ERR}"

BASENAME="$(basename "${INPUT}")"
jq -n \
    --arg original "${BASENAME}" \
    --arg sha256 "${SHA256}" \
    --arg sha256_strip "${SHA256_STRIP}" \
    --arg sha256_webp "${SHA256_WEBP}" \
    --arg uuid "${UUID}" \
    '{
        original_filename: $original,
        sha256: $sha256,
        sha256_strip: $sha256_strip,
        sha256_webp: $sha256_webp,
        uuid: $uuid
    }' >metadata.json ||
    error_exit "failed to generate metadata"

TAR_ERR=$(tar -cJf "../${UUID}.tar.xz" -C . . 2>&1) ||
    error_exit "failed to create archive: ${TAR_ERR}"

jq -n --arg success "${UUID}" '{"success": $success}'
