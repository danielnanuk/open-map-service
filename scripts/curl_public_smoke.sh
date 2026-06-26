#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-https://map.wildog.net}"
API_KEY="${API_KEY:-}"

headers=(-H "Content-Type: application/json")
if [[ -n "$API_KEY" ]]; then
  headers+=(-H "X-Goog-Api-Key: $API_KEY")
fi

request() {
  local name="$1"
  shift

  echo
  echo "==> $name"
  curl --fail-with-body --show-error --silent --location --max-time 30 "$@"
  echo
}

request "healthz" \
  "$BASE_URL/healthz"

request "places searchText" \
  -X POST "$BASE_URL/v1/places:searchText" \
  "${headers[@]}" \
  -H "X-Goog-FieldMask: places.id,places.displayName,places.formattedAddress" \
  --data '{"textQuery":"Angkor Wat","languageCode":"en"}'

request "geocode address" \
  "$BASE_URL/maps/api/geocode/json?address=Street+271,+Phnom+Penh&language=en"

request "reverse geocode" \
  "$BASE_URL/maps/api/geocode/json?latlng=11.5621,104.9160&language=en"

request "routes tuktuk" \
  -X POST "$BASE_URL/directions/v2:computeRoutes" \
  "${headers[@]}" \
  --data '{"origin":{"location":{"latLng":{"latitude":11.5564,"longitude":104.9282}}},"destination":{"location":{"latLng":{"latitude":11.5696,"longitude":104.9210}}},"travelMode":"TWO_WHEELER","vehicleProfile":"tuktuk"}'

request "matrix 2x2 tuktuk" \
  -X POST "$BASE_URL/distanceMatrix/v2:computeRouteMatrix" \
  "${headers[@]}" \
  --data '{"origins":[{"waypoint":{"location":{"latLng":{"latitude":11.5564,"longitude":104.9282}}}},{"waypoint":{"location":{"latLng":{"latitude":11.5696,"longitude":104.9210}}}}],"destinations":[{"waypoint":{"location":{"latLng":{"latitude":11.5984,"longitude":104.9192}}}},{"waypoint":{"location":{"latLng":{"latitude":11.5625,"longitude":104.9311}}}}],"travelMode":"TWO_WHEELER","vehicleProfile":"tuktuk"}'
