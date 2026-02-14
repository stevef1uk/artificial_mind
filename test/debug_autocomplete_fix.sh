#!/bin/bash
# Test the autocomplete exclusion logic in headless_browser

BROWSER_TOOL="/home/stevef/dev/artificial_mind/tools/headless_browser/main.go"

echo "🧪 Case 1: Airport field (should trigger autocomplete logic)"
go run $BROWSER_TOOL -url "https://ecotree.green/en/calculators/flight" \
  -actions '[{"type": "fill", "selector": "#flight_calculator_to", "value": "CDG"}]' \
  -timeout 60 2>&1 | grep "Waiting for autocomplete"

echo "🧪 Case 2: Aircraft type field (should NOT trigger autocomplete logic)"
go run $BROWSER_TOOL -url "https://ecotree.green/en/calculators/flight" \
  -actions '[{"type": "fill", "selector": "#flight_calculator_aircraft_type_leg_1", "value": "Boeing 737"}]' \
  -timeout 60 2>&1 | grep "Waiting for autocomplete"

echo "🧪 Case 3: Mixed (should only trigger for the right one)"
go run $BROWSER_TOOL -url "https://ecotree.green/en/calculators/flight" \
  -actions '[{"type": "fill", "selector": "#flight_calculator_from", "value": "LHR"}, {"type": "fill", "selector": "#flight_calculator_aircraft_type_leg_1", "value": "Boeing 737"}]' \
  -timeout 60 2>&1 | grep -E "Filling|Waiting for autocomplete"
