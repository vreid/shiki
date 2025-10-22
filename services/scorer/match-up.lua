local rankingKey = KEYS[1]
local countKey = KEYS[2]
local numAssets = tonumber(ARGV[1])

local ratings = {}
local counts = {}
for i = 1, numAssets do
    local asset = ARGV[1 + i]
    ratings[i] = tonumber(redis.call('ZSCORE', rankingKey, asset) or 1500)
    counts[i] = tonumber(redis.call('ZSCORE', countKey, asset) or 0)
end

local function getKFactor(gamesPlayed)
    if gamesPlayed <= 20 then
        return 128.0
    elseif gamesPlayed <= 50 then
        return 64.0
    else
        return 32.0
    end
end

local function calculateExpectedScore(ratingA, ratingB)
    return 1.0 / (1.0 + math.pow(10, (ratingB - ratingA) / 400.0))
end

local winnerRating = ratings[1]
local newRatings = {}
newRatings[1] = winnerRating

for j = 2, numAssets do
    local loserRating = ratings[j]

    local winnerK = getKFactor(counts[1])
    local loserK = getKFactor(counts[j])
    local k = math.max(winnerK, loserK)

    local expectedWinner = calculateExpectedScore(winnerRating, loserRating)

    local winnerChange = k * (1.0 - expectedWinner)
    local loserChange = k * (0.0 - (1.0 - expectedWinner))

    winnerRating = winnerRating + winnerChange
    loserRating = loserRating + loserChange

    newRatings[1] = winnerRating
    newRatings[j] = loserRating

    counts[1] = counts[1] + 1
    counts[j] = counts[j] + 1
end

for i = 1, numAssets do
    local asset = ARGV[1 + i]
    redis.call('ZADD', rankingKey, newRatings[i], asset)
    redis.call('ZADD', countKey, counts[i], asset)
end

return 'OK'
