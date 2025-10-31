#!/bin/bash

set -euo pipefail

MATCHMAKER_URL="${MATCHMAKER_URL:-http://localhost:3000/api/matchmaker}"

run_client() {
    echo "Getting initial match-up..."
    MATCHUP_RESPONSE=$(curl -s "${MATCHMAKER_URL}/match-up")

    ROUND=0

    while true; do
        ROUND=$((ROUND + 1))

        SIGNATURE=$(echo "${MATCHUP_RESPONSE}" | jq -r '.signature')
        OPPONENTS=$(echo "${MATCHUP_RESPONSE}" | jq -c '.match_up.opponents')
        DIFFICULTY=$(echo "${MATCHUP_RESPONSE}" | jq -r '.match_up.difficulty')

        OPPONENT_COUNT=$(echo "${OPPONENTS}" | jq 'length')
        RANDOM_INDEX=$((RANDOM % OPPONENT_COUNT))
        WINNER_ID=$(echo "${OPPONENTS}" | jq -r ".[${RANDOM_INDEX}].opponent_id")

        echo "$(date -Iseconds) Round ${ROUND} | Difficulty: ${DIFFICULTY}, Winner: ${WINNER_ID}"

        NONCE=0
        MESSAGE="${SIGNATURE}|${WINNER_ID}|${NONCE}"
        HASH=$(echo -n "${MESSAGE}" | sha256sum | awk '{print $1}')

        if [[ "${DIFFICULTY}" -gt 0 ]]; then
            #echo "Computing proof of work..."
            TARGET=$(printf '0%.0s' $(seq 1 "${DIFFICULTY}"))

            while true; do
                MESSAGE="${SIGNATURE}|${WINNER_ID}|${NONCE}"
                HASH=$(echo -n "${MESSAGE}" | sha256sum | awk '{print $1}')

                if [[ "${HASH:0:${DIFFICULTY}}" == "${TARGET}" ]]; then
                    #echo "Found valid nonce: ${NONCE}, Hash: ${HASH}"
                    break
                fi

                NONCE=$((NONCE + 1))

                if ((NONCE % 1000 == 0)); then
                    echo "$(date -Iseconds) Tried ${NONCE} nonces..."
                fi
            done
        fi

        OUTCOME=$(jq -n \
            --argjson matchup "${MATCHUP_RESPONSE}" \
            --arg winner_id "${WINNER_ID}" \
            --argjson nonce "${NONCE}" \
            --arg hash "${HASH}" \
            '{
                match_up: $matchup,
                winner_id: $winner_id,
                nonce: $nonce,
                hash: $hash
            }')

        MATCHUP_RESPONSE=$(curl -s -X POST \
            -H "Content-Type: application/json" \
            -d "${OUTCOME}" \
            "${MATCHMAKER_URL}/outcome")
    done
}

export -f run_client
export MATCHMAKER_URL

seq 32 | parallel -j 32 run_client
