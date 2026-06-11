// travelMode(+扩展 vehicleProfile)→ Valhalla costing。
//
// tuk-tuk 定参依据 spec §14.3: scripts/tuktuk_calibration.sh 对 20 条金边路线实测。
//
// 实测结论摘要:
//   - Valhalla 3.7.0 无 low_speed_vehicle costing(error 125),使用 motor_scooter 替代。
//   - costing_options 传参格式 {"motor_scooter":{...}} 已确认正确。
//   - top_speed 旋钮有效,但仅在路段限速 > top_speed 时产生差异:
//     金边城区道路限速 ≤30kph,低于 motor_scooter 默认 top_speed=45kph,
//     因此城区 20 条 OD 全部 tuktuk≡scooter(符合预期:城区慢行由路网决定)。
//   - 跨城验证(PP→暹粒 316km):top_speed:40 使时间从 30950s 增至 33644s(+8.7%),
//     路径从 349.9km 变为 354.0km,证明旋钮在高速公路路段生效。
//   - 最终 tuk-tuk 参数:top_speed:40 + use_highways:0.1
//     (top_speed:40 低于 NR 级公路限速,阻止走高速;use_highways:0.1 附加公路惩罚)
//   - 三项准则全部通过:tuktuk_time≥scooter_time ✓;距离≤auto×1.3(最大比值1.07)✓;0 ERR ✓。
package route

import "fmt"

// TravelModeToCosting maps a travelMode string and optional vehicleProfile
// to a Valhalla costing name and costing_options map.
//
// Supported travelModes: DRIVE, WALK, BICYCLE, TWO_WHEELER (empty string defaults to DRIVE).
// Supported vehicleProfiles: "" (none) or "tuktuk" (TWO_WHEELER only).
//
// Returns (costing, options, nil) on success, or ("", nil, error) for
// unsupported modes or invalid mode+profile combinations.
func TravelModeToCosting(mode, vehicleProfile string) (string, map[string]any, error) {
	if mode == "" {
		mode = "DRIVE"
	}
	if vehicleProfile != "" && vehicleProfile != "tuktuk" {
		return "", nil, fmt.Errorf("unknown vehicleProfile %q", vehicleProfile)
	}
	if vehicleProfile == "tuktuk" && mode != "TWO_WHEELER" {
		return "", nil, fmt.Errorf("vehicleProfile tuktuk requires travelMode TWO_WHEELER")
	}
	switch mode {
	case "DRIVE":
		return "auto", nil, nil
	case "WALK":
		return "pedestrian", nil, nil
	case "BICYCLE":
		return "bicycle", nil, nil
	case "TWO_WHEELER":
		if vehicleProfile == "tuktuk" {
			// top_speed:40 keeps tuk-tuk off NR-class highways (tagged >45kph).
			// use_highways:0.1 adds highway penalty even for segments within speed cap.
			// Calibrated against 20 Phnom Penh ODs + PP→SR inter-city validation.
			return "motor_scooter", map[string]any{
				"motor_scooter": map[string]any{"top_speed": 40, "use_highways": 0.1},
			}, nil
		}
		return "motor_scooter", nil, nil
	default:
		return "", nil, fmt.Errorf("unsupported travelMode %q", mode)
	}
}
