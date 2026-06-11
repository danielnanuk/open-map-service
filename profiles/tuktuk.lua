-- profiles/tuktuk.lua
-- Tuk-tuk profile for Cambodia: hard cap all speeds at 40 km/h (aligns with M3 Valhalla
-- costing top_speed=40). Uses vehicle_max_speed so the cap applies to all derived speeds
-- (surface, maxspeed tags, etc.), not just the base highway table.
--
-- api_version = 4: OSRM requires return { setup, process_way, process_node, process_turn }.

api_version = 4

local car = dofile('/opt/car.lua')

function setup()
  local profile = car.setup()

  -- Cap base highway speeds so routing weights are correct (not just result clamping)
  local hw = profile.speeds.highway
  for k, v in pairs(hw) do
    hw[k] = math.min(v, 40)
  end

  -- vehicle_max_speed is the authoritative cap applied by WayHandlers.vehicle_speed_cap
  -- to forward/backward speeds after all other handlers (surface, maxspeed, etc.)
  profile.vehicle_max_speed = 40

  return profile
end

return {
  setup        = setup,
  process_way  = car.process_way,
  process_node = car.process_node,
  process_turn = car.process_turn,
}
