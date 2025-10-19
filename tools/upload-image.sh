#!/usr/bin/env bash

set -e -u -o pipefail

curl -X POST \
    -F files=@img/IMG_20200320_192744.jpg \
    -F files=@img/IMG_20200322_145243.jpg \
    -F files=@img/IMG_20200327_153733.jpg \
    -F files=@img/IMG_20200328_220021.jpg \
    http://localhost:3001/upload
