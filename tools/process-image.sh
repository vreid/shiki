#!/usr/bin/env bash

set -e -u -o pipefail

INPUT="${1:?}"

TEMP="$(mktemp -d -p .)"
trap 'rm -rf "${TEMP}"' EXIT

SHA256=$(sha256sum "${INPUT}" | awk '{print $1}')
cp "${INPUT}" "${TEMP}/${SHA256}"

cd "${TEMP}"

INPUT="${SHA256}"

convert "${INPUT}" -strip "${SHA256}_strip" 2>/dev/null
SHA256_STRIP=$(sha256sum "${SHA256}_strip" | awk '{print $1}')
mv "${SHA256}_strip" "${SHA256_STRIP}"

convert "${SHA256}" -strip "${SHA256}.webp" 2>/dev/null

SHA256_WEBP="$(sha256sum "${SHA256}.webp" | awk '{print $1}')"
UUID="$(uuidgen -s -N "${SHA256_WEBP}" -n @oid)"

if [ -f "../${UUID}.tar.xz" ]; then
    echo "${UUID}"
    exit 0
fi

mv "${SHA256}.webp" "${UUID}.webp"

convert "${UUID}.webp" \
    -colors 16 \
    -depth 8 \
    -format "%c" \
    histogram:info:- 2>/dev/null |
    sort -rn >"${UUID}.hist"

convert "${UUID}.webp" \
    -resize 1024x1024 \
    -gravity center \
    -background transparent \
    -extent 1024x1024 \
    -strip \
    "${UUID}_1024.webp" 2>/dev/null

QUALITY="50"
for SIZE in 1024 512 256 128 64 32; do
    convert "${UUID}_1024.webp" \
        -resize ${SIZE}x${SIZE} \
        -quality "${QUALITY}" \
        "${UUID}_${SIZE}_${QUALITY}.webp" 2>/dev/null
done

exiftool -json "${INPUT}" >"${SHA256}.json" 2>/dev/null
exiftool -json "${SHA256_STRIP}" >"${SHA256_STRIP}.json" 2>/dev/null
exiftool -json "${UUID}.webp" >"${UUID}.json" 2>/dev/null
exiftool -json "${UUID}_1024.webp" >"${UUID}_1024.json" 2>/dev/null

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
    }' >metadata.json

tar -cJf "../${UUID}.tar.xz" -C . . 2>/dev/null

echo "${UUID}"
