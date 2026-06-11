-- profiles/moto.lua
-- Motorbike profile for Cambodia: highway speed caps lowered (motorists typically <=60 km/h),
-- smaller roads unchanged. Uses vehicle_max_speed for simplicity but also overrides
-- highway table so routing weights reflect real speeds, not just result clamping.
--
-- Strategy: dofile car.lua to reuse all logic; override setup() to patch speeds.
-- api_version = 4: OSRM requires return { setup, process_way, process_node, process_turn }.

api_version = 4

-- Load car module; dofile returns the table { setup, process_way, process_node, process_turn }
local car = dofile('/opt/car.lua')

function setup()
  local profile = car.setup()

  -- Override highway speeds: motorway/trunk/primary/secondary lowered to Cambodia moto norms
  local hw = profile.speeds.highway
  hw.motorway        = 60
  hw.motorway_link   = 45
  hw.trunk           = 60
  hw.trunk_link      = 40
  hw.primary         = 55
  hw.primary_link    = 28
  hw.secondary       = 50
  hw.secondary_link  = 22

  return profile
end

return {
  setup        = setup,
  process_way  = car.process_way,
  process_node = car.process_node,
  process_turn = car.process_turn,
}
