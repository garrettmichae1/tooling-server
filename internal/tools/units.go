package tools

import "encoding/json"

type unit struct {
	dimension     string
	scale, offset float64
}

// Base temperature is kelvin: base = value * scale + offset.
var units = map[string]unit{
	"mm": {"length", .001, 0}, "cm": {"length", .01, 0}, "m": {"length", 1, 0}, "km": {"length", 1000, 0},
	"in": {"length", .0254, 0}, "ft": {"length", .3048, 0}, "yd": {"length", .9144, 0}, "mi": {"length", 1609.344, 0},
	"mg": {"mass", .000001, 0}, "g": {"mass", .001, 0}, "kg": {"mass", 1, 0}, "oz": {"mass", .028349523125, 0}, "lb": {"mass", .45359237, 0},
	"s": {"duration", 1, 0}, "min": {"duration", 60, 0}, "h": {"duration", 3600, 0}, "day": {"duration", 86400, 0},
	"mL": {"volume", .001, 0}, "L": {"volume", 1, 0}, "tsp": {"volume", .00492892159375, 0}, "tbsp": {"volume", .01478676478125, 0},
	"fl_oz": {"volume", .0295735295625, 0}, "cup": {"volume", .2365882365, 0}, "gal": {"volume", 3.785411784, 0},
	"m/s": {"speed", 1, 0}, "km/h": {"speed", 1.0 / 3.6, 0}, "mph": {"speed", .44704, 0},
	"m2": {"area", 1, 0}, "ft2": {"area", .09290304, 0}, "acre": {"area", 4046.8564224, 0}, "ha": {"area", 10000, 0},
	"C": {"temperature", 1, 273.15}, "F": {"temperature", 5.0 / 9.0, 273.15 - 32*5.0/9.0}, "K": {"temperature", 1, 0},
}

var unitNames = []string{"mm", "cm", "m", "km", "in", "ft", "yd", "mi", "mg", "g", "kg", "oz", "lb", "s", "min", "h", "day", "mL", "L", "tsp", "tbsp", "fl_oz", "cup", "gal", "m/s", "km/h", "mph", "m2", "ft2", "acre", "ha", "C", "F", "K"}

func convertUnits(raw json.RawMessage) (any, error) {
	var in struct {
		Value float64 `json:"value"`
		From  string  `json:"from"`
		To    string  `json:"to"`
	}
	decode(raw, &in)
	a, b := units[in.From], units[in.To]
	if a.dimension != b.dimension {
		return nil, invalid("arguments.to", "from and to must have the same dimension")
	}
	base := in.Value*a.scale + a.offset
	if a.dimension == "temperature" && base < -1e-10 {
		return nil, invalid("arguments.value", "absolute temperature cannot be below zero kelvin")
	}
	if a.dimension == "temperature" && base < 0 {
		base = 0
	}
	value := (base - b.offset) / b.scale
	return map[string]any{"value": value, "unit": in.To, "dimension": a.dimension, "input_value": in.Value, "input_unit": in.From}, nil
}
